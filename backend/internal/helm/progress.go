package helm

import (
	"context"
	"io"
)

// Download progress, for the install dialog's progress bar. A caller that wants
// it attaches a callback to the context; the catalog reports bytes received so
// far and the total when the server said how big the download is (total is -1
// when it didn't). Reporting is best-effort and never affects the download.

type progressKey struct{}

// WithProgress returns a context whose chart downloads report to fn.
func WithProgress(ctx context.Context, fn func(done, total int64)) context.Context {
	return context.WithValue(ctx, progressKey{}, fn)
}

func reportProgress(ctx context.Context, done, total int64) {
	if fn, ok := ctx.Value(progressKey{}).(func(done, total int64)); ok {
		fn(done, total)
	}
}

// countingReader reports every read to the context's progress callback.
type countingReader struct {
	ctx   context.Context
	r     io.Reader
	done  int64
	total int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.done += int64(n)
		reportProgress(c.ctx, c.done, c.total)
	}
	return n, err
}
