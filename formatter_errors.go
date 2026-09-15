package xlog

import (
	"bufio"
	"io"
	"sync"

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

type formatterErrors struct {
	mu  sync.Mutex
	err error
}

// Err returns the first recorded formatter error and is safe during logging.
func (e *formatterErrors) Err() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}

func (e *formatterErrors) record(err error) {
	if err == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err == nil {
		e.err = err
	}
}

func (e *formatterErrors) flushDestination(dest any) {
	if flusher, ok := dest.(interface{ Flush() error }); ok {
		e.record(errors.WithMessage(flusher.Flush(), "unable to flush log destination"))
	}
}
