package xlog

import (
	"bufio"
	"io"
	"time"

	"github.com/cockroachdb/errors"
)

func newFormatterBuffer(dest io.Writer) *bufio.Writer {
	if buffered, ok := dest.(*bufio.Writer); ok {
		// Preserve bufio.NewWriter's existing-buffer reuse semantics.
		return bufio.NewWriter(buffered)
	}
	// MultiWriter turns short writes without errors into io.ErrShortWrite.
	// Otherwise bufio's large-write path can retry a zero-progress writer forever.
	return bufio.NewWriter(io.MultiWriter(dest))
}

// ErrorFormatter augments Formatter with delivery error reporting. Built-in
// output formatters implement it without changing the legacy Formatter interface.
type ErrorFormatter interface {
	Formatter
	// Err returns the first encoding or destination error, or nil. It is safe
	// to call concurrently with formatting. Errors remain available for the
	// lifetime of the formatter; successful later records do not clear them.
	Err() error
	// FlushError flushes formatter and supported downstream buffers, returning
	// the first recorded error. Like Format, it requires caller synchronization.
	// This is a delivery barrier for ChannelWriter destinations, not an fsync.
	FlushError() error
}

// FlushFormatterWithin flushes f like FlushFormatter, bounding the wait when f
// supports it. Formatters delivering through a Sink report a timeout instead of
// waiting for a stalled destination; inline formatters have no queue to drain,
// so they ignore the limit. A non-positive limit waits indefinitely.
func FlushFormatterWithin(f Formatter, limit time.Duration) error {
	if f == nil {
		return nil
	}
	if bounded, ok := f.(interface{ FlushWithin(time.Duration) error }); ok {
		return errors.WithMessage(bounded.FlushWithin(limit), "unable to flush formatter")
	}
	return FlushFormatter(f)
}

// FlushFormatter flushes f and supported downstream buffers. Nil is a no-op.
// Legacy formatters without FlushError are flushed but cannot report errors.
// Direct callers must synchronize with formatting and destination shutdown.
func FlushFormatter(f Formatter) error {
	if f == nil {
		return nil
	}
	if checked, ok := f.(interface{ FlushError() error }); ok {
		return errors.WithMessage(checked.FlushError(), "unable to flush formatter")
	}
	f.Flush()
	return nil
}
