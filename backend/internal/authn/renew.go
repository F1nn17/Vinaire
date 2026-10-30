package authn

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// Renewing a session (Roadmap Phase 84).
//
// A Kubernetes credential obtained at sign-in is short-lived on purpose. Without renewal that made a session
// as short as the credential (a person working was thrown back to the login screen), or — to avoid that —
// made the credential long, so that someone disabled in Keycloak kept working until it ran out. Now the
// credential is short (RenewableTokenTTL) and the session is long (the configured maximum); while the person
// is active, shortly before the credential would run out, Vinaire asks the identity provider whether the
// person still has a session there (the refresh token) and, if so, obtains a new credential:
//
//   - the provider says no (the user was disabled, the SSO session ended, the refresh token was revoked or
//     expired): the Vinaire session ends at once;
//   - the provider can't be reached: nothing is known, so the session goes on — for RenewGrace since the
//     provider last said yes, no longer;
//   - the person's Kubernetes account is gone (an administrator deleted it): the session ends — a renewal
//     never creates an account, so deleting one is a way to cut a person off;
//   - the provider answers with a different subject than the session was made for: the session ends.
//
// The refresh token stays on the server (in memory, or sealed in the shared store) like the credential
// itself; it is never sent to the browser, listed or logged.
const (
	// RenewableTokenTTL is the lifetime of the Kubernetes credential of a session that can be renewed.
	RenewableTokenTTL = 30 * time.Minute
	// RenewGrace is how long a session may go on without the provider having confirmed the person.
	RenewGrace = time.Hour
)

// renewTimeout bounds each of the two calls of a renewal — the one to the identity provider and the one that obtains
// the new Kubernetes credential — separately: a stuck provider must not hang the request that triggered it, and
// must not use up the time the second call needs either. A variable so that a test can make it short.
var renewTimeout = 15 * time.Second

// renewSaveBackoff is how long a renewal keeps trying to save a session whose refresh token the provider has just
// rotated, while the store is unreachable (Roadmap Phase 112): the stored copy still holds the spent token, so losing
// the new one ends the session for good at the next renewal. A var so tests can shorten it.
var renewSaveBackoff = []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second}

// renewSaveBudget caps the time the retries above may take once the first save has failed: no retry starts after it.
// Together with the call timeout of the store it bounds how long a renewal can hold the renewal lease (renewLeaseTTL).
var renewSaveBudget = 20 * time.Second

// renewSaveWorstCase is the longest a saveRenewed can take: the first call, the retries that start within the budget, and
// the last of them still running to its own timeout.
func renewSaveWorstCase() time.Duration { return renewSaveBudget + 2*storeCallTimeout }

// saveRenewed saves a renewed session, retrying only while the store is unavailable and only within renewSaveBudget.
func (m *Manager) saveRenewed(next *Session) error {
	start := time.Now()
	err := m.Sessions.Update(next)
	for _, wait := range renewSaveBackoff {
		if !errors.Is(err, ErrStoreUnavailable) {
			return err
		}
		if time.Since(start)+wait > renewSaveBudget {
			break
		}
		time.Sleep(wait)
		err = m.Sessions.Update(next)
	}
	return err
}

// ErrAccountGone is what a TokenRenewer returns when the person's Kubernetes account no longer exists.
var ErrAccountGone = errors.New("the person's Kubernetes account no longer exists")

// TokenRenewer is implemented by a broker that can mint a new credential for a person who already has an
// account — and must never create one.
type TokenRenewer interface {
	RenewToken(ctx context.Context, id Identity) (BrokeredToken, error)
}

// (one group per Manager — that is, per replica: the lease below is what keeps two replicas apart)

// RenewalCoordinator is implemented by a session store that several replicas share (Roadmap Phase 95): it lets one of
// them be the only one renewing a session, and lets the others see the result instead of presenting a spent refresh token.
type RenewalCoordinator interface {
	AcquireRenewal(id string, ttl time.Duration) (release func(), acquired bool, err error)
	Fresh(id string) (*Session, bool)
}

// vars so that a test can shorten them
var (
	// renewLeaseMargin is what the lease allows on top of the longest renewal.
	renewLeaseMargin = 10 * time.Second
	// renewWait is how long a replica waits for another one's renewal before carrying on with the credential it has.
	renewWait     = 20 * time.Second
	renewWaitStep = 150 * time.Millisecond
)

// renewLeaseTTL is how long a replica holds the right to present a session's refresh token. It must outlive the WORST renewal —
// the call to the provider, the call that obtains the new credential and the save of the rotated refresh token with its
// retries — or a second replica could take the lease while the first is still trying to save, and present the token the
// provider has just rotated away (Roadmap Phase 114). It then lapses by itself, so a replica that dies half-way does not block
// the session for long.
func renewLeaseTTL() time.Duration {
	return 2*renewTimeout + renewSaveWorstCase() + renewLeaseMargin
}

// needsRenewal says whether a session should be renewed now.
func needsRenewal(s *Session, now time.Time, lead time.Duration) bool {
	return s.RefreshToken != "" && !s.TokenExpiresAt.IsZero() && s.TokenExpiresAt.Sub(now) < lead
}

// renewLead: a credential with less than a third of its life left is renewed on the next request — so one that
// is used regularly never runs out, and one that is not isn't renewed for nothing.
func (m *Manager) renewLead() time.Duration { return m.tokenTTL() / 3 }

// renewIfDue returns the session to use for this request: the same one, a renewed one, or false when the
// session has ended. Concurrent requests for one session share a single renewal.
func (m *Manager) renewIfDue(ctx context.Context, sess *Session) (*Session, bool) {
	if !needsRenewal(sess, time.Now(), m.renewLead()) {
		return sess, true
	}
	v, _, _ := m.renewals.Do(sess.ID, func() (any, error) {
		// another request may have renewed it while this one waited its turn
		if cur, ok := m.Sessions.Get(sess.ID); ok {
			if !needsRenewal(cur, time.Now(), m.renewLead()) {
				return cur, nil
			}
			sess = cur
		} else {
			return (*Session)(nil), nil
		}
		if coord, ok := m.Sessions.(RenewalCoordinator); ok {
			return m.renewCoordinated(ctx, coord, sess), nil
		}
		return m.renew(ctx, sess), nil
	})
	out, _ := v.(*Session)
	if out == nil {
		return nil, false
	}
	return out, true
}

// renewCoordinated renews a session that other replicas may be renewing too: only the replica that holds the lease
// presents the refresh token; the others wait for its result. nil means the session has ended.
func (m *Manager) renewCoordinated(ctx context.Context, coord RenewalCoordinator, sess *Session) *Session {
	release, got, err := coord.AcquireRenewal(sess.ID, renewLeaseTTL())
	if err != nil {
		// the store can't be reached, so a rotated refresh token could not be saved either: don't rotate it
		slog.Warn("session renewal: couldn't coordinate with the other replicas — trying again on a later request", "error", err)
		return sess
	}
	if !got {
		// another replica is on it: wait for the renewed session rather than present a token it is about to spend
		deadline := time.Now().Add(renewWait)
		for time.Now().Before(deadline) {
			time.Sleep(renewWaitStep)
			cur, ok := coord.Fresh(sess.ID)
			if !ok {
				return nil // it ended (or the other replica ended it)
			}
			if !needsRenewal(cur, time.Now(), m.renewLead()) {
				return cur
			}
		}
		return sess // the other replica is slow or gone: carry on with the credential we have
	}
	defer release()
	// we hold the lease; another replica may have renewed this session just before we got it
	cur, ok := coord.Fresh(sess.ID)
	if !ok {
		return nil
	}
	if !needsRenewal(cur, time.Now(), m.renewLead()) {
		return cur
	}
	return m.renew(ctx, cur)
}

// renew does the work; nil means the session has been ended.
func (m *Manager) renew(ctx context.Context, sess *Session) *Session {
	oc := m.getOIDC()
	if oc == nil {
		return sess // the sign-in was switched off: nothing to ask; the credential just runs out
	}
	// A renewal belongs to the session, not to the request that happened to start it: if that request goes away
	// half-way, a refresh token the provider has already rotated must still be kept (below), so the work is not
	// cancelled with it. Each outward call gets its own deadline.
	ctx = context.WithoutCancel(ctx)
	provCtx, cancel := context.WithTimeout(ctx, renewTimeout)
	defer cancel()

	next := *sess
	validated := false
	tok, err := oc.oauth2Config.TokenSource(oc.ctx(provCtx), &oauth2.Token{RefreshToken: sess.RefreshToken}).Token()
	switch {
	case err == nil:
		validated = true
		if tok.RefreshToken != "" {
			next.RefreshToken = tok.RefreshToken
		}
		raw, _ := tok.Extra("id_token").(string)
		if raw != "" {
			idToken, verr := oc.verifier.Verify(oc.ctx(provCtx), raw)
			if verr != nil || idToken.Subject != sess.Subject || idToken.Issuer != sess.Issuer {
				m.end(sess, "the identity provider answered for someone else, or with a token that doesn't verify")
				return nil
			}
			if broker := m.IdentityBroker(); broker == nil {
				next.K8sToken, next.TokenExpiresAt = raw, idToken.Expiry
			}
		} else if m.IdentityBroker() == nil {
			// the ID token itself is the credential and the provider didn't send a new one
			m.end(sess, "the identity provider sent no new ID token")
			return nil
		}
		next.ValidatedAt = time.Now()
	case isRefusal(err):
		m.end(sess, "the identity provider no longer accepts this person's session")
		return nil
	default:
		// can't reach the provider: carry on for a while, but only on a credential Vinaire can renew by itself
		if time.Since(sess.ValidatedAt) > RenewGrace {
			m.end(sess, "the identity provider has been unreachable for longer than the grace period")
			return nil
		}
		slog.Warn("session renewal: the identity provider didn't answer — continuing within the grace period", "error", err)
	}

	if broker := m.IdentityBroker(); broker != nil {
		renewer, ok := broker.(TokenRenewer)
		if !ok {
			return m.keep(sess, &next, validated)
		}
		kctx, kcancel := context.WithTimeout(ctx, renewTimeout)
		defer kcancel()
		bt, rerr := renewer.RenewToken(kctx, Identity{Issuer: sess.Issuer, Subject: sess.Subject, Username: sess.Username, TokenTTL: m.tokenTTL()})
		if rerr != nil {
			if errors.Is(rerr, ErrAccountGone) {
				m.end(sess, "the person's Kubernetes account was deleted")
				return nil
			}
			slog.Warn("session renewal: couldn't get a new Kubernetes credential — the current one stays until it runs out", "error", rerr)
			return m.keep(sess, &next, validated)
		}
		next.K8sToken, next.TokenExpiresAt, next.KubernetesUser = bt.Token, bt.Expiry, bt.KubernetesUser
	} else if !validated {
		// the ID token is the credential and the provider can't be reached: nothing new to hold
		return &next
	}
	if err := m.saveRenewed(&next); err != nil {
		if errors.Is(err, ErrNoSession) { // revoked (or ended) while it was being renewed: it stays ended
			slog.Info("session renewal: the session was ended meanwhile; the renewed credential is dropped")
			return nil
		}
		slog.Warn("session renewal: couldn't save the renewed session", "error", err)
		return sess
	}
	return &next
}

// keep is for a renewal that did not produce a new Kubernetes credential: the session goes on with the one it has. If
// the provider answered, though, it has ROTATED the refresh token — the old one is spent — so the new one is saved now;
// returning it unsaved would lose it, and the next renewal would be refused with invalid_grant and end the session.
func (m *Manager) keep(old, next *Session, providerAnswered bool) *Session {
	if !providerAnswered {
		return next
	}
	if err := m.saveRenewed(next); err != nil {
		if errors.Is(err, ErrNoSession) {
			return nil
		}
		slog.Warn("session renewal: couldn't save the rotated refresh token", "error", err)
		return old
	}
	return next
}

// end removes a session whose renewal failed for good. The reason goes to the log without anything that
// identifies a credential.
func (m *Manager) end(sess *Session, reason string) {
	m.Sessions.Delete(sess.ID)
	slog.Warn("session ended at renewal", "reason", reason, "user", sess.Username)
}

// isRefusal tells the provider's "no" (the refresh token is no longer good) from "couldn't ask".
func isRefusal(err error) bool {
	var re *oauth2.RetrieveError
	if !errors.As(err, &re) {
		return false
	}
	if re.ErrorCode == "invalid_grant" || re.ErrorCode == "unauthorized_client" || re.ErrorCode == "invalid_client" {
		return true
	}
	// a 4xx without an OAuth error code is still the provider answering "no", not a failure to ask
	return re.Response != nil && re.Response.StatusCode >= 400 && re.Response.StatusCode < 500 && !strings.Contains(string(re.Body), "temporarily")
}
