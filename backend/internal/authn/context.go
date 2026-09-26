package authn

import "context"

type contextKey int

const sessionKey contextKey = iota

func withSession(ctx context.Context, sess *Session) context.Context {
	return context.WithValue(ctx, sessionKey, sess)
}

// FromContext retrieves the Session Middleware attached to this request.
func FromContext(ctx context.Context) (*Session, bool) {
	sess, ok := ctx.Value(sessionKey).(*Session)
	return sess, ok
}
