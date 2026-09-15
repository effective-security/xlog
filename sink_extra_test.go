package xlog_test

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/effective-security/xlog"
	"github.com/stretchr/testify/require"
)

// countingWriter is safe to read while the worker writes.
type countingWriter struct {
	dest    io.Writer
	writes  atomic.Int64
	flushes atomic.Int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes.Add(1)
	return w.dest.Write(p)
}

func (w *countingWriter) Flush() error {
	w.flushes.Add(1)
	return nil
}

// A periodic interval flushes destinations without an explicit barrier.
func TestSinkFlushInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out bytes.Buffer
		w := &countingWriter{dest: &out}
		sink, err := xlog.NewSink(4, xlog.WithFlushInterval(time.Second))
		require.NoError(t, err)
		f := xlog.NewStringFormatter(w, xlog.FormatSkipTime(true), xlog.WithSink(sink))
		f.Format("", xlog.INFO, 0, "tick")
		synctest.Wait()
		require.Zero(t, w.flushes.Load(), "no barrier has been requested yet")
		time.Sleep(2 * time.Second)
		synctest.Wait()
		require.Positive(t, w.flushes.Load(), "the interval must flush the destination")
		require.Contains(t, out.String(), "tick")
		require.NoError(t, sink.Close())
	})
}

// A small batch budget writes every record separately; the default coalesces.
func TestSinkBatchBytes(t *testing.T) {
	const records = 16
	var out bytes.Buffer
	w := &countingWriter{dest: &out}
	sink := newTestSink(t, records+1, xlog.WithBatchBytes(1))
	f := xlog.NewStringFormatter(w, xlog.FormatSkipTime(true), xlog.WithSink(sink))
	for range records {
		f.Format("", xlog.INFO, 0, "record")
	}
	require.NoError(t, sink.Close())
	require.Equal(t, records, strings.Count(out.String(), "\n"))
	require.EqualValues(t, records, w.writes.Load())
}

// A bufio.Writer destination keeps the constructor's buffer-reuse semantics.
func TestSinkBufferedDestination(t *testing.T) {
	var out bytes.Buffer
	downstream := bufio.NewWriter(&out)
	sink := newTestSink(t, 4)
	f := xlog.NewStringFormatter(downstream, xlog.FormatSkipTime(true), xlog.WithSink(sink))
	f.Format("", xlog.INFO, 0, "buffered")
	require.NoError(t, sink.Flush(), "the barrier flushes through to the destination")
	require.Contains(t, out.String(), "buffered")
	require.NoError(t, sink.Close())
}

func TestSinkStateAndDestAccessors(t *testing.T) {
	var out bytes.Buffer
	sink, err := xlog.NewSink(2)
	require.NoError(t, err)
	require.False(t, sink.IsClosed())
	f := xlog.NewStringFormatter(&out, xlog.FormatSkipTime(true), xlog.WithSink(sink))
	bound, ok := f.(interface{ Dest() io.Writer })
	require.True(t, ok)
	require.Same(t, &out, bound.Dest())
	f.Format("", xlog.INFO, 0, "state")
	f.Flush() // the void barrier of the Formatter interface
	require.Contains(t, out.String(), "state")
	require.NoError(t, sink.Close())
	require.True(t, sink.IsClosed())
	// A bounded barrier after shutdown reports the cached shutdown result.
	require.NoError(t, sink.FlushWithin(time.Minute))
}
