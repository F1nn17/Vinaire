package provision

import (
	"context"
	"errors"
	"fmt"
)

// Roadmap Phase 35: Reset undoes a Kubernetes install on a machine — for one
// whose install failed partway (an undefined state that otherwise can't be
// retried), or one being taken out of its cluster. It is destructive, so the
// HTTP layer gates it behind typing the machine's address; everything about
// handling SSH credentials is the same as for the other runs.

// ResetRequest is everything a reset needs.
type ResetRequest struct {
	Provider string
	Node     Target
}

// StartReset runs the reset in the background and returns immediately.
func StartReset(job *Job, req ResetRequest, onDone func(err error)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
		defer cancel()

		red := NewRedactor(req.Node.Creds.Password, req.Node.Creds.PrivateKey, req.Node.Creds.Passphrase)
		log := func(line string) { job.Log(red.Redact(line)) }

		err := runReset(ctx, job, req, log)

		req.Node.Creds = Credentials{}
		if err != nil {
			err = errors.New(red.Redact(err.Error()))
			job.finish(StatusFailed, err.Error(), "")
		} else {
			job.finish(StatusSucceeded, "", "")
		}
		if onDone != nil {
			onDone(err)
		}
	}()
}

func runReset(ctx context.Context, job *Job, req ResetRequest, log LogFunc) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("internal error: %v", rec)
		}
	}()
	provider, err := ProviderByName(req.Provider)
	if err != nil {
		return err
	}
	job.Step("Connecting to " + req.Node.Host)
	addr, err := address(req.Node)
	if err != nil {
		return err
	}
	node, err := Dial(ctx, addr, req.Node.Creds, req.Node.HostKeyFingerprint)
	if err != nil {
		return err
	}
	defer node.Close()
	log("connected as " + req.Node.Creds.Username)

	job.Step("Resetting " + provider.Name() + " on this machine")
	if err := provider.Reset(ctx, node, log); err != nil {
		return fmt.Errorf("reset failed: %w", err)
	}
	return nil
}
