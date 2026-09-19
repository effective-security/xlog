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

// A full queue behind a stalled destination must not hold Fatal forever: the
// CRITICAL record is dropped once CriticalFlushTimeout elapses.
func TestSinkCriticalAdmissionIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newGate()
		sink, err := xlog.NewSink(1)
		require.NoError(t, err)
		f := xlog.NewStringFormatter(w, xlog.FormatSkipTime(true), xlog.WithSink(sink))
		remove := xlog.InstallFormatter(f)
		l := xlog.NewPackageLogger("sink-tests", "critical-admission")
		l.Info("gate")
		<-w.started // the worker is stuck in the destination write
		l.Info("queued")
		exits := 0
		oldExit := xlog.ExitFunc
		xlog.ExitFunc = func(code int) { exits++; require.Equal(t, 1, code) }
		start := time.Now()
		l.Fatal("dropped")
		elapsed := time.Since(start)
		xlog.ExitFunc = oldExit

		require.Equal(t, 1, exits, "Fatal must reach ExitFunc")
		require.Positive(t, elapsed, "admission must have waited for the bound")
		require.LessOrEqual(t, elapsed, 2*xlog.CriticalFlushTimeout,
			"admission and the delivery barrier are each bounded once")
		require.ErrorContains(t, f.(xlog.ErrorFormatter).Err(), "queue space")
		require.Equal(t, uint64(1), sink.Stats().Dropped)

		close(w.release)
		remove()
		require.NoError(t, sink.Close())
		require.NotContains(t, w.String(), "dropped")
	})
}

// A bounded barrier must observe its own limit even while an unbounded barrier
// is already waiting on the stalled destination.
func TestSinkBoundedBarrierIgnoresUnboundedBarrier(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newGate()
		sink, err := xlog.NewSink(4)
		require.NoError(t, err)
		f := xlog.NewStringFormatter(w, xlog.FormatSkipTime(true), xlog.WithSink(sink))
		f.Format("", xlog.INFO, 0, "stuck")
		<-w.started
		unbounded := make(chan error, 1)
		go func() { unbounded <- sink.Flush() }()
		synctest.Wait() // the unbounded barrier now holds the serialization token
		require.ErrorContains(t, sink.FlushWithin(time.Second), "timed out")
		close(w.release)
		require.NoError(t, <-unbounded)
		require.NoError(t, sink.Close())
	})
}

// A timed-out wait is not a delivery failure and must not poison Err.
func TestSinkFlushTimeoutIsNotSticky(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newGate()
		sink, err := xlog.NewSink(4)
		require.NoError(t, err)
		f := xlog.NewStringFormatter(w, xlog.FormatSkipTime(true), xlog.WithSink(sink))
		bounded, ok := f.(interface {
			FlushWithin(time.Duration) error
		})
		require.True(t, ok)
		f.Format("", xlog.INFO, 0, "slow")
		<-w.started
		require.ErrorContains(t, bounded.FlushWithin(time.Second), "timed out")
		require.NoError(t, f.(xlog.ErrorFormatter).Err())
		close(w.release)
		require.NoError(t, sink.Close())
		require.NoError(t, f.(xlog.ErrorFormatter).Err(), "the drain succeeded after the timeout")
		require.Contains(t, w.String(), "slow")
	})
}

type flushPanicWriter struct {
	bytes.Buffer
}

func (w *flushPanicWriter) Flush() error { panic("flush panic") }

// A panicking destination flush must not take the worker down with it.
func TestSinkDestinationFlushPanic(t *testing.T) {
	w := &flushPanicWriter{}
	sink := newTestSink(t, 2)
	f := xlog.NewStringFormatter(w, xlog.FormatSkipTime(true), xlog.WithSink(sink))
	f.Format("", xlog.INFO, 0, "one")
	require.ErrorContains(t, sink.Flush(), "destination panicked")
	f.Format("", xlog.INFO, 0, "two")
	require.Error(t, sink.Close())
	require.Contains(t, w.String(), "one")
	require.Contains(t, w.String(), "two", "the worker kept draining")
}

// Shutdown outranks the record-size policy.
func TestSinkOversizeAfterClose(t *testing.T) {
	var out bytes.Buffer
	sink, err := xlog.NewSink(2, xlog.WithMaxRecordBytes(64))
	require.NoError(t, err)
	f := xlog.NewStringFormatter(&out,
		xlog.FormatSkipTime(true), xlog.FormatWithCaller(false),
		xlog.FormatMaxLogLength(-1), xlog.WithSink(sink)).(xlog.ErrorFormatter)
	require.NoError(t, sink.Close())
	f.Format("", xlog.INFO, 0, strings.Repeat("x", 512))
	require.ErrorIs(t, f.Err(), io.ErrClosedPipe)
	require.Zero(t, sink.Stats().Oversize, "stats must not change after shutdown")
	require.Empty(t, out.String())
}
