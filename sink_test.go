package xlog_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/xlog"
	"github.com/effective-security/xlog/stackdriver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var sinkConstructors = []struct {
	name string
	new  func(io.Writer, ...xlog.FormatterOption) xlog.Formatter
}{
	{"text", xlog.NewStringFormatter},
	{"pretty", xlog.NewPrettyFormatter},
	{"json", xlog.NewJSONFormatter},
	{"stackdriver", func(w io.Writer, ops ...xlog.FormatterOption) xlog.Formatter {
		return stackdriver.NewFormatter(w, "service", ops...)
	}},
}

// gateWriter blocks its first write until released, so tests can hold the sink
// worker at the destination. All writes happen on the worker: inspect the
// buffer only after a barrier or Close.
type gateWriter struct {
	bytes.Buffer
	started  chan struct{}
	release  chan struct{}
	once     sync.Once
	writes   int
	flushes  int
	closed   bool
	flushErr error
}

func (w *gateWriter) Write(p []byte) (int, error) {
	w.once.Do(func() {
		close(w.started)
		<-w.release
	})
	w.writes++
	return w.Buffer.Write(p)
}

func (w *gateWriter) Flush() error { w.flushes++; return w.flushErr }
func (w *gateWriter) Close() error { w.closed = true; return nil }

func newGate() *gateWriter {
	return &gateWriter{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

// newTestSink returns a sink closed by the test, failing on shutdown errors.
func newTestSink(t *testing.T, capacity int, opts ...xlog.SinkOption) *xlog.Sink {
	t.Helper()
	sink, err := xlog.NewSink(capacity, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sink.Close() })
	return sink
}

func TestSinkParity(t *testing.T) {
	for _, constructor := range sinkConstructors {
		for _, allOptions := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/options=%t", constructor.name, allOptions), func(t *testing.T) {
				var inline, queued bytes.Buffer
				options := []xlog.FormatterOption{
					xlog.FormatWithCaller(allOptions), xlog.FormatWithLocation(allOptions),
					xlog.FormatWithColor(allOptions), xlog.FormatPrintEmpty(allOptions),
					xlog.FormatSkipTime(!allOptions), xlog.FormatSkipLevel(!allOptions),
					xlog.FormatMaxLogLength(12),
				}
				sink := newTestSink(t, 2)
				formatters := []xlog.Formatter{
					constructor.new(&inline, options...),
					constructor.new(&queued, append(options, xlog.WithSink(sink))...),
				}
				for _, formatter := range formatters {
					remove := xlog.InstallFormatter(formatter)
					sinkParityLogs()
					remove()
					require.NoError(t, xlog.FlushFormatter(formatter))
				}
				require.NoError(t, sink.Close())
				require.Equal(t, inline.String(), queued.String())
				require.NotEmpty(t, queued.String())
			})
		}
	}
}

func sinkParityLogs() {
	l := xlog.NewPackageLogger("sink-tests", "parity")
	l.Info("plain", 42, "<>&", "", "end\n")
	l.Infof("printf %d", 42)
	l.KV(xlog.INFO, "empty", "", "nil", nil, "dup", 1, "dup", 2,
		"map", map[string]any{"html": "<>&"}, "bytes", []byte("hello"),
		"long", strings.Repeat("x", 30), "trailing")
	l.KV(xlog.ERROR, "reason", "failure")
	l.WithValues("attached", 1).(*xlog.PackageLogger).Log(xlog.INFO, "derived")
	l.ContextKV(xlog.ContextWithKV(context.Background(), "context", 2), xlog.INFO, "local", 3)
	l.Log(xlog.CRITICAL, "critical")
}

type mutableValue struct{ value string }

func (v *mutableValue) String() string { return v.value }
func (v *mutableValue) Error() string  { return v.value }

type mutableJSON struct{ data []byte }

func (v *mutableJSON) MarshalJSON() ([]byte, error) { return v.data, nil }

// The record is rendered before the logging call returns, so later mutation of
// caller-owned values and of the clock cannot reach the queued record.
func TestSinkOwnsValuesAndMetadata(t *testing.T) {
	for _, constructor := range sinkConstructors {
		t.Run(constructor.name, func(t *testing.T) {
			w := newGate()
			sink := newTestSink(t, 4)
			f := constructor.new(w, xlog.FormatWithLocation(true), xlog.WithSink(sink))
			l := xlog.NewPackageLogger("sink-tests", "snapshot")
			remove := xlog.InstallFormatter(f)
			t.Cleanup(remove)
			l.Info("gate")
			<-w.started
			oldClock := xlog.TimeNowFn
			t.Cleanup(func() { xlog.TimeNowFn = oldClock })
			stamp := time.Date(2026, 9, 15, 12, 34, 56, 0, time.UTC)
			xlog.TimeNowFn = func() time.Time { return stamp }
			mapping := map[string]any{"value": "before"}
			payload := []byte("before")
			raw := json.RawMessage(`"before"`)
			value := &mutableValue{value: "before"}
			custom := &mutableJSON{data: []byte(`"before"`)}
			entries := []any{"map", mapping, "bytes", payload, "raw", raw, "error", value, "custom", custom}
			l.KV(xlog.INFO, entries...)
			mapping["value"] = "after"
			copy(payload, "after!")
			copy(raw, `"after!"`)
			copy(custom.data, `"after!"`)
			value.value = "after"
			entries[1] = "replaced"
			xlog.TimeNowFn = func() time.Time { return stamp.Add(time.Hour) }
			close(w.release)
			require.NoError(t, sink.Close())
			assert.Contains(t, w.String(), "before")
			assert.NotContains(t, w.String(), "after")
			assert.NotContains(t, w.String(), "replaced")
			assert.Contains(t, w.String(), "12:34:56")
			assert.NotContains(t, w.String(), "13:34:56")
			assert.Contains(t, w.String(), "TestSinkOwnsValuesAndMetadata")
			assert.Contains(t, w.String(), "sink_test.go")
		})
	}
}

func TestSinkBackpressureAndClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newGate()
		sink, err := xlog.NewSink(1)
		require.NoError(t, err)
		f := xlog.NewStringFormatter(w, xlog.WithSink(sink)).(xlog.ErrorFormatter)
		f.Format("", xlog.INFO, 0, "first")
		<-w.started // The worker holds the first record at the destination.
		f.Format("", xlog.INFO, 0, "second")
		third := make(chan struct{})
		go func() { f.Format("", xlog.INFO, 0, "third"); close(third) }()
		synctest.Wait()
		select {
		case <-third:
			t.Fatal("full queue did not block the producer")
		default:
		}
		require.Equal(t, 1, sink.Stats().QueuedRecords)
		const closers = 4
		results := make(chan error, closers)
		for range closers {
			go func() { results <- sink.Close() }()
		}
		synctest.Wait()
		<-third // Close releases a rejected producer before the destination recovers.
		select {
		case <-results:
			t.Fatal("Close returned before accepted records drained")
		default:
		}
		close(w.release)
		for range closers {
			require.NoError(t, <-results)
		}
		require.Equal(t, 2, strings.Count(w.String(), "\n"))
		require.Contains(t, w.String(), "first")
		require.Contains(t, w.String(), "second")
		require.NotContains(t, w.String(), "third")
		require.False(t, w.closed, "destinations stay owned by the application")
		require.Equal(t, 1, w.flushes)
		require.ErrorIs(t, f.Err(), io.ErrClosedPipe)
		require.NoError(t, sink.Err(), "a rejection is not a delivery failure")
		stats := sink.Stats()
		require.Equal(t, uint64(2), stats.Submitted)
		require.Equal(t, uint64(2), stats.Written)
		require.Zero(t, stats.QueuedRecords)
		require.Zero(t, stats.QueuedBytes)
	})
}

// The byte budget bounds retained memory before the record count does.
func TestSinkQueueBytesBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newGate()
		sink, err := xlog.NewSink(100, xlog.WithQueueBytes(1))
		require.NoError(t, err)
		f := xlog.NewStringFormatter(w, xlog.WithSink(sink))
		f.Format("", xlog.INFO, 0, "first")
		<-w.started
		// One record over budget is admitted alone, never two.
		f.Format("", xlog.INFO, 0, "second")
		third := make(chan struct{})
		go func() { f.Format("", xlog.INFO, 0, "third"); close(third) }()
		synctest.Wait()
		select {
		case <-third:
			t.Fatal("byte budget did not block the producer")
		default:
		}
		require.Equal(t, 1, sink.Stats().QueuedRecords, "capacity alone would allow 100")
		close(w.release)
		<-third
		require.NoError(t, sink.Close())
		require.Equal(t, 3, strings.Count(w.String(), "\n"))
	})
}

func TestSinkOversizeRecord(t *testing.T) {
	var out bytes.Buffer
	sink := newTestSink(t, 4, xlog.WithMaxRecordBytes(64))
	f := xlog.NewStringFormatter(&out,
		xlog.FormatSkipTime(true), xlog.FormatWithCaller(false),
		xlog.FormatMaxLogLength(-1), xlog.WithSink(sink)).(xlog.ErrorFormatter)
	f.Format("", xlog.INFO, 0, strings.Repeat("x", 512))
	require.NoError(t, sink.Flush())
	require.Empty(t, out.String())
	require.ErrorContains(t, f.Err(), "the limit is 64")
	require.Equal(t, uint64(1), sink.Stats().Oversize)
	f.Format("", xlog.INFO, 0, "small")
	require.NoError(t, sink.Close())
	require.Contains(t, out.String(), "small")

	_, err := xlog.NewSink(4, xlog.WithMaxRecordBytes(1<<20), xlog.WithQueueBytes(1024))
	require.ErrorContains(t, err, "exceeds its queue budget")
}

func TestSinkOverflowDropNewest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newGate()
		sink, err := xlog.NewSink(1, xlog.WithOverflow(xlog.OverflowDropNewest))
		require.NoError(t, err)
		f := xlog.NewStringFormatter(w, xlog.WithSink(sink)).(xlog.ErrorFormatter)
		f.Format("", xlog.INFO, 0, "first")
		<-w.started
		f.Format("", xlog.INFO, 0, "second")
		f.Format("", xlog.INFO, 0, "dropped")
		f.Format("", xlog.INFO, 0, "dropped")
		require.Equal(t, uint64(2), sink.Stats().Dropped)
		require.NoError(t, f.Err(), "a selected lossy policy is not an error")
		close(w.release)
		require.NoError(t, sink.Close())
		require.Equal(t, 2, strings.Count(w.String(), "\n"))
		require.NotContains(t, w.String(), "dropped")
	})
}

func TestSinkFlushBarrier(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newGate()
		sink, err := xlog.NewSink(2)
		require.NoError(t, err)
		f := xlog.NewJSONFormatter(w, xlog.WithSink(sink)).(xlog.ErrorFormatter)
		f.Format("", xlog.INFO, 0, "one")
		<-w.started
		f.Format("", xlog.INFO, 0, "two")
		flushed := make(chan error, 1)
		go func() { flushed <- f.FlushError() }()
		synctest.Wait()
		select {
		case <-flushed:
			t.Fatal("Flush returned before delivery")
		default:
		}
		close(w.release)
		require.NoError(t, <-flushed)
		require.Equal(t, 2, strings.Count(w.String(), "\n"))
		require.Equal(t, 1, w.flushes)
		require.NoError(t, sink.Close())
		f.Format("", xlog.INFO, 0, "rejected")
		require.ErrorIs(t, f.Err(), io.ErrClosedPipe)
		require.NoError(t, sink.Close(), "the shutdown result stays stable")
		require.NotContains(t, w.String(), "rejected")
	})
}

// A stalled destination must not block process exit or an explicit barrier.
func TestSinkBoundedFlushAndClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newGate()
		sink, err := xlog.NewSink(4)
		require.NoError(t, err)
		f := xlog.NewStringFormatter(w, xlog.WithSink(sink))
		bounded, ok := f.(interface {
			FlushWithin(time.Duration) error
		})
		require.True(t, ok)
		f.Format("", xlog.INFO, 0, "stuck")
		<-w.started
		require.ErrorContains(t, bounded.FlushWithin(time.Second), "timed out")
		require.ErrorContains(t, sink.FlushWithin(time.Second), "timed out")
		require.ErrorContains(t, sink.CloseWithin(time.Second), "timed out")
		close(w.release)
		require.NoError(t, sink.Close(), "a bounded close still drains in the background")
		require.Contains(t, w.String(), "stuck")
	})
}

func TestSinkConcurrentProducers(t *testing.T) {
	var out bytes.Buffer
	// Serialize the shared buffer behind the single worker only.
	sink := newTestSink(t, 3)
	f := xlog.NewJSONFormatter(&out, xlog.WithSink(sink))
	require.True(t, f.(xlog.ConcurrentFormatter).Concurrent())
	remove := xlog.InstallFormatter(f)
	t.Cleanup(remove)
	l := xlog.NewPackageLogger("sink-tests", "concurrent")
	const producers, records = 8, 100
	var wg sync.WaitGroup
	for producer := range producers {
		wg.Go(func() {
			for seq := range records {
				l.KV(xlog.INFO, "producer", producer, "seq", seq)
			}
		})
	}
	wg.Go(func() {
		for range records {
			assert.NoError(t, l.FlushError())
		}
	})
	wg.Wait()
	remove()
	require.NoError(t, sink.Close())
	decoder := json.NewDecoder(&out)
	counts := make([]int, producers)
	for range producers * records {
		var record struct{ Producer, Seq int }
		require.NoError(t, decoder.Decode(&record))
		require.Equal(t, counts[record.Producer], record.Seq, "per-producer order is preserved")
		counts[record.Producer]++
	}
	for _, count := range counts {
		require.Equal(t, records, count)
	}
	var extra any
	require.ErrorIs(t, decoder.Decode(&extra), io.EOF)
}

// The worker coalesces queued records, so a burst costs far fewer writes.
func TestSinkBatchesWrites(t *testing.T) {
	const records = 64
	w := newGate()
	sink := newTestSink(t, records+1)
	f := xlog.NewStringFormatter(w, xlog.WithSink(sink))
	f.Format("", xlog.INFO, 0, "first")
	<-w.started
	for i := range records {
		f.Format("", xlog.INFO, 0, "record", i)
	}
	close(w.release)
	require.NoError(t, sink.Close())
	require.Equal(t, records+1, strings.Count(w.String(), "\n"))
	require.Less(t, w.writes, records, "records must be coalesced into batches")
}

func TestSinkSharedByFormatters(t *testing.T) {
	var text, structured bytes.Buffer
	sink := newTestSink(t, 8)
	plain := xlog.NewStringFormatter(&text, xlog.FormatSkipTime(true), xlog.WithSink(sink))
	encoded := xlog.NewJSONFormatter(&structured, xlog.FormatSkipTime(true), xlog.WithSink(sink))
	for i := range 4 {
		plain.Format("pkg", xlog.INFO, 0, "text", i)
		encoded.FormatKV("pkg", xlog.INFO, 0, "seq", i)
	}
	require.NoError(t, sink.Close())
	require.Equal(t, 4, strings.Count(text.String(), "\n"))
	require.Equal(t, 4, strings.Count(structured.String(), "\n"))
	require.NotContains(t, text.String(), "{")
	require.Contains(t, structured.String(), `"seq":3`)
}

func TestSinkErrors(t *testing.T) {
	for _, capacity := range []int{0, -1} {
		_, err := xlog.NewSink(capacity)
		require.ErrorContains(t, err, "capacity must be positive")
	}
	for _, constructor := range sinkConstructors {
		t.Run(constructor.name, func(t *testing.T) {
			for _, failure := range []error{io.ErrShortWrite, io.ErrUnexpectedEOF} {
				sink := newTestSink(t, 1)
				f := constructor.new(callbackWriter(func([]byte) (int, error) {
					if failure == io.ErrShortWrite {
						return 0, nil
					}
					return 0, failure
				}), xlog.WithSink(sink)).(xlog.ErrorFormatter)
				f.Format("", xlog.INFO, 0, strings.Repeat("x", 64<<10))
				require.ErrorIs(t, f.FlushError(), failure)
				require.ErrorIs(t, f.Err(), failure)
				require.ErrorIs(t, sink.Close(), failure)
			}
			w := newGate()
			w.flushErr = errors.New("flush failed")
			close(w.release)
			sink := newTestSink(t, 1)
			f := constructor.new(w, xlog.WithSink(sink))
			f.Format("", xlog.INFO, 0, "message")
			require.ErrorIs(t, sink.Close(), w.flushErr)
		})
	}
}

// Rendering runs on the producer, so invalid input panics there, and the sink
// keeps working afterwards.
func TestSinkRenderPanicStaysOnProducer(t *testing.T) {
	for _, constructor := range sinkConstructors {
		t.Run(constructor.name, func(t *testing.T) {
			var out bytes.Buffer
			sink := newTestSink(t, 2)
			f := constructor.new(&out, xlog.WithSink(sink)).(xlog.ErrorFormatter)
			require.Panics(t, func() { f.FormatKV("", xlog.INFO, 0, 123, "bad key") })
			f.FormatKV("", xlog.INFO, 0, "good", "valid")
			require.NoError(t, f.FlushError(), "a producer panic must not wedge the sink")
			require.Contains(t, out.String(), "valid")
			if constructor.name == "json" || constructor.name == "stackdriver" {
				f.FormatKV("", xlog.INFO, 0, "bad", make(chan int))
				var unsupported *json.UnsupportedTypeError
				require.ErrorAs(t, f.FlushError(), &unsupported)
				require.NoError(t, sink.Close(), "an encoding failure is not a delivery failure")
			} else {
				require.Panics(t, func() { f.Format("", xlog.LogLevel(99), 0, "bad level") })
				require.NoError(t, sink.Close())
			}
		})
	}
}

type panicWriter struct {
	bytes.Buffer
	remaining int
}

func (w *panicWriter) Write(p []byte) (int, error) {
	if w.remaining > 0 {
		w.remaining--
		panic("destination panic")
	}
	return w.Buffer.Write(p)
}

func TestSinkDestinationPanic(t *testing.T) {
	w := &panicWriter{remaining: 1}
	sink := newTestSink(t, 2)
	f := xlog.NewStringFormatter(w,
		xlog.FormatSkipTime(true), xlog.FormatSkipLevel(true),
		xlog.FormatWithCaller(false), xlog.WithSink(sink))
	f.Format("pkg", xlog.INFO, 0, "boom")
	require.ErrorContains(t, sink.Flush(), "destination panicked")
	f.Format("pkg", xlog.INFO, 0, "after")
	require.Error(t, sink.Close())
	assert.Contains(t, w.String(), "after", "the worker keeps draining after a panic")
	// The batch buffer is untouched by a panicking destination, so the record
	// it was holding is written by the next flush rather than being lost.
	assert.Contains(t, w.String(), "boom")
}

func TestSinkOptionsRebindAndNil(t *testing.T) {
	var inline, queued bytes.Buffer
	sink := newTestSink(t, 2)
	f := xlog.NewStringFormatter(&inline,
		xlog.FormatSkipTime(true), xlog.FormatSkipLevel(true), xlog.FormatWithCaller(false))
	require.False(t, f.(xlog.ConcurrentFormatter).Concurrent())
	f.Format("pkg", xlog.INFO, 0, "inline")
	require.Contains(t, inline.String(), "inline")

	// Rebinding keeps the destination and moves delivery onto the worker.
	require.Same(t, f, f.Options(xlog.WithSink(sink)))
	require.True(t, f.(xlog.ConcurrentFormatter).Concurrent())
	f.Format("pkg", xlog.INFO, 0, "queued")
	require.NoError(t, xlog.FlushFormatter(f))
	require.Contains(t, inline.String(), "queued")

	// Rebinding back to inline delivery leaves earlier records with the sink.
	require.Same(t, f, f.Options(xlog.WithSink(nil)))
	require.False(t, f.(xlog.ConcurrentFormatter).Concurrent())
	require.Empty(t, queued.String())
	require.NoError(t, sink.Close())

	nilFormatter := xlog.NewNilFormatter()
	require.True(t, nilFormatter.(xlog.ConcurrentFormatter).Concurrent())
	nilFormatter.FormatKV("", xlog.INFO, 0, 123, make(chan int))
	nilFormatter.Flush()
}

func TestSinkCriticalAndReplacement(t *testing.T) {
	var out bytes.Buffer
	// Hide bufio.Writer so the destination keeps a distinct downstream buffer.
	downstream := struct{ *bufio.Writer }{bufio.NewWriter(&out)}
	sink := newTestSink(t, 2)
	f := xlog.NewStringFormatter(downstream, xlog.WithSink(sink))
	remove := xlog.InstallFormatter(f)
	t.Cleanup(remove)
	l := xlog.NewPackageLogger("sink-tests", "critical")
	oldExit := xlog.ExitFunc
	t.Cleanup(func() { xlog.ExitFunc = oldExit })
	xlog.ExitFunc = func(code int) {
		require.Equal(t, 1, code)
		require.Contains(t, out.String(), "fatal", "CRITICAL waits for delivery before exit")
	}
	l.Fatal("fatal")
	l.Fatalf("fatal %d", 2)
	require.PanicsWithValue(t, "panic", func() { l.Panic("panic") })
	require.Contains(t, out.String(), "panic")
	require.PanicsWithValue(t, "panic 2", func() { l.Panicf("panic %d", 2) })

	var replacement bytes.Buffer
	newRemove := xlog.InstallFormatter(xlog.NewStringFormatter(&replacement))
	t.Cleanup(newRemove)
	remove()
	require.NoError(t, sink.Close())
	l.Info("replacement")
	require.Contains(t, replacement.String(), "replacement")
	require.NotContains(t, out.String(), "replacement")
}

func TestSinkJSONReservedFields(t *testing.T) {
	w := newGate()
	close(w.release)
	sink := newTestSink(t, 4)
	f := xlog.NewJSONFormatter(w, xlog.WithSink(sink))
	value := &mutableValue{value: "before"}
	f.Format("", xlog.INFO, 0, value)
	// Unsupported values on overwritten keys must not reject the record.
	bad := make(chan int)
	f.FormatKV("pkg", xlog.ERROR, 0, "time", bad, "level", bad,
		"pkg", bad, "src", bad, "func", bad, "present", nil)
	value.value = "after"
	require.NoError(t, sink.Close())
	decoder := json.NewDecoder(&w.Buffer)
	var record map[string]any
	require.NoError(t, decoder.Decode(&record))
	require.Equal(t, "before", record["msg"])
	record = nil
	require.NoError(t, decoder.Decode(&record))
	require.Equal(t, "pkg", record["pkg"])
	require.Equal(t, "E", record["level"])
	require.Contains(t, record, "present")
	require.Nil(t, record["present"])
	require.NotContains(t, record, "msg")
}
