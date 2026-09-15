package xlog_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/xlog"
	"github.com/effective-security/xlog/logrotate"
	"github.com/effective-security/xlog/stackdriver"
	"github.com/stretchr/testify/require"
)

const (
	perfRepo       = "xlog-benchmark"
	perfPackage    = "pipeline"
	perfSmall      = 64
	perfMedium     = 2 << 10
	perfLarge      = 64 << 10
	perfSamples    = 4096
	perfQueueDepth = 256
	perfDelay      = time.Millisecond
)

type pipelineCase struct {
	format   string
	api      string
	sink     string
	fields   int
	size     int
	caller   bool
	parallel bool
}

func (c pipelineCase) name() string {
	return fmt.Sprintf("%s/%s/%s/fields=%d/bytes=%d/caller=%t/parallel=%t",
		c.format, c.api, c.sink, c.fields, c.size, c.caller, c.parallel)
}

// BenchmarkSyncPipeline measures encoding, serialization, destination writes and
// final flush. Payload construction and logger/context creation are excluded.
func BenchmarkSyncPipeline(b *testing.B) {
	for _, format := range []string{"text", "pretty", "json", "stackdriver"} {
		for _, caller := range []bool{false, true} {
			for _, parallel := range []bool{false, true} {
				for _, fields := range []int{0, 4, 16} {
					for _, size := range []int{perfSmall, perfMedium, perfLarge} {
						c := pipelineCase{
							format:   format,
							api:      "kv",
							sink:     "discard",
							fields:   fields,
							size:     size,
							caller:   caller,
							parallel: parallel,
						}
						b.Run(c.name(), func(b *testing.B) { benchmarkPipeline(b, c, false) })
					}
				}
			}
		}
	}
}

// BenchmarkSyncAPI isolates the public API and disabled-level costs.
func BenchmarkSyncAPI(b *testing.B) {
	for _, api := range []string{"plain", "printf", "kv", "context", "derived", "disabled", "disabled-context"} {
		for _, parallel := range []bool{false, true} {
			c := pipelineCase{
				format:   "text",
				api:      api,
				sink:     "discard",
				fields:   4,
				size:     perfSmall,
				caller:   true,
				parallel: parallel,
			}
			if api == "plain" || api == "printf" {
				c.fields = 0
			}
			b.Run(c.name(), func(b *testing.B) { benchmarkPipeline(b, c, false) })
		}
	}
}

// BenchmarkSyncSinks compares the same record through synchronous destinations.
func BenchmarkSyncSinks(b *testing.B) {
	for _, sink := range []string{"discard", "file", "rotation", "slow", "failing"} {
		for _, parallel := range []bool{false, true} {
			c := pipelineCase{
				format:   "pretty",
				api:      "kv",
				sink:     sink,
				fields:   4,
				size:     perfSmall,
				caller:   true,
				parallel: parallel,
			}
			b.Run(c.name(), func(b *testing.B) { benchmarkPipeline(b, c, false) })
		}
	}
}

// BenchmarkSyncLatency samples producer call latency separately so clock reads
// and sample storage do not contaminate the throughput/allocation baseline.
func BenchmarkSyncLatency(b *testing.B) {
	for _, format := range []string{"text", "json", "stackdriver"} {
		for _, sink := range []string{"discard", "file", "slow"} {
			for _, size := range []int{perfSmall, perfMedium, perfLarge} {
				for _, parallel := range []bool{false, true} {
					c := pipelineCase{
						format:   format,
						api:      "kv",
						sink:     sink,
						fields:   4,
						size:     size,
						caller:   true,
						parallel: parallel,
					}
					b.Run(c.name(), func(b *testing.B) { benchmarkPipeline(b, c, true) })
				}
			}
		}
	}
}

func perfGlobals(tb testing.TB) *xlog.PackageLogger {
	tb.Helper()
	oldClock := xlog.TimeNowFn
	oldLimit := stackdriver.MaxLogMessageLength
	xlog.TimeNowFn = time.Now
	stackdriver.MaxLogMessageLength = perfLarge * 2
	tb.Cleanup(func() {
		xlog.TimeNowFn = oldClock
		stackdriver.MaxLogMessageLength = oldLimit
	})
	l := xlog.NewPackageLogger(perfRepo, perfPackage)
	xlog.SetPackageLogLevel(perfRepo, perfPackage, xlog.INFO)
	return l
}

func perfFormatter(format string, w io.Writer, caller bool, ops ...xlog.FormatterOption) xlog.Formatter {
	options := append([]xlog.FormatterOption{
		xlog.FormatWithCaller(caller), xlog.FormatWithLocation(false),
		xlog.FormatWithColor(false), xlog.FormatMaxLogLength(-1),
	}, ops...)
	switch format {
	case "text":
		return xlog.NewStringFormatter(w, options...)
	case "pretty":
		return xlog.NewPrettyFormatter(w, options...)
	case "json":
		return xlog.NewJSONFormatter(w, options...)
	case "stackdriver":
		return stackdriver.NewFormatter(w, perfRepo, options...)
	default:
		panic("unknown benchmark formatter")
	}
}

func perfEmit(l *xlog.PackageLogger, c pipelineCase) func() {
	message := strings.Repeat("x", c.size)
	fields := make([]any, 0, c.fields*2)
	for i := 0; i < c.fields; i++ {
		fields = append(fields, fmt.Sprintf("field%d", i), i+1)
	}
	entries := append([]any{"msg", message}, fields...)
	ctx := xlog.ContextWithKV(context.Background(), fields...)
	derived := l.WithValues(fields...)
	switch c.api {
	case "plain":
		return func() { l.Info(message) }
	case "printf":
		return func() { l.Infof("%s", message) }
	case "context":
		return func() { l.ContextKV(ctx, xlog.INFO, "msg", message) }
	case "derived":
		return func() { derived.KV(xlog.INFO, "msg", message) }
	case "disabled":
		return func() { l.KV(xlog.DEBUG, entries...) }
	case "disabled-context":
		return func() { l.ContextKV(ctx, xlog.DEBUG, "msg", message) }
	default:
		return func() { l.KV(xlog.INFO, entries...) }
	}
}

// Access is serialized by xlog (or by the single byte worker). Read after drain.
type perfWriter struct {
	dest    io.Writer
	delay   time.Duration
	fail    bool
	records int64
	bytes   int64
	writes  int64
}

func (w *perfWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.delay > 0 {
		time.Sleep(w.delay)
	}
	if w.fail {
		return 0, errors.WithMessage(io.ErrClosedPipe, "benchmark injected write failure")
	}
	n, err := w.dest.Write(p)
	w.records += int64(bytes.Count(p[:n], []byte{'\n'}))
	w.bytes += int64(n)
	return n, errors.WithMessage(err, "unable to write benchmark record")
}

func perfDestination(b *testing.B, c pipelineCase) (*perfWriter, func() int64) {
	b.Helper()
	w := &perfWriter{dest: io.Discard}
	closeSink := func() error { return nil }
	records := func() int64 { return w.records }
	switch c.sink {
	case "file":
		f, err := os.Create(filepath.Join(b.TempDir(), "records.log"))
		require.NoError(b, err)
		w.dest = f
		closeSink = f.Close
	case "rotation":
		dir := b.TempDir()
		closer, err := logrotate.Initialize(dir, "records", 1, 1024, false, nil)
		require.NoError(b, err)
		closeSink = closer.Close
		// Count actual file records after the final buffer flush; keep this scan
		// outside the benchmark timer. MaxSize avoids rollover in short runs.
		records = func() int64 {
			f, err := os.Open(filepath.Join(dir, "records.log"))
			require.NoError(b, err)
			counter := &perfWriter{dest: io.Discard}
			_, err = io.Copy(counter, f)
			require.NoError(b, err)
			require.NoError(b, f.Close())
			w.bytes = counter.bytes
			return counter.records
		}
	case "slow":
		w.delay = perfDelay
	case "failing":
		w.fail = true
	}
	if c.sink != "rotation" {
		remove := xlog.InstallFormatter(perfFormatter(c.format, w, c.caller))
		b.Cleanup(remove)
	}
	b.Cleanup(func() { require.NoError(b, closeSink()) })
	return w, records
}

func benchmarkPipeline(b *testing.B, c pipelineCase, sample bool) {
	l := perfGlobals(b)
	w, records := perfDestination(b, c)
	emit := perfEmit(l, c)
	workers := 1
	if c.parallel {
		workers = runtime.GOMAXPROCS(0)
	}
	samples := make([][]time.Duration, workers)
	for i := range samples {
		if sample {
			samples[i] = make([]time.Duration, 0, perfSamples)
		}
	}
	// Fixed per-worker partitions avoid a shared benchmark counter/lock.
	work := func(worker int) {
		count := b.N / workers
		if worker < b.N%workers {
			count++
		}
		stride := max(1, (count+perfSamples-1)/perfSamples)
		for i := 0; i < count; i++ {
			if sample && i%stride == 0 {
				start := time.Now()
				emit()
				samples[worker] = append(samples[worker], time.Since(start))
			} else {
				emit()
			}
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	if workers == 1 {
		work(0)
	} else {
		var wg sync.WaitGroup
		start := make(chan struct{})
		for worker := 0; worker < workers; worker++ {
			wg.Go(func() {
				<-start
				work(worker)
			})
		}
		close(start)
		wg.Wait()
	}
	flushStart := time.Now()
	err := l.FlushError()
	drain := time.Since(flushStart)
	b.StopTimer()
	if c.sink == "failing" {
		require.ErrorIs(b, err, io.ErrClosedPipe)
	} else {
		require.NoError(b, err)
	}
	delivered := records()
	expected := int64(b.N)
	if strings.HasPrefix(c.api, "disabled") || c.sink == "failing" {
		expected = 0
	}
	require.Equal(b, expected, delivered)
	b.ReportMetric(float64(delivered)/b.Elapsed().Seconds(), "records/s")
	b.ReportMetric(float64(drain.Nanoseconds()), "drain-ns")
	b.ReportMetric(float64(w.bytes)/float64(b.N), "output-B/op")
	if sample {
		var all []time.Duration
		for _, s := range samples {
			all = append(all, s...)
		}
		slices.Sort(all)
		for _, percentile := range []int{50, 95, 99} {
			b.ReportMetric(float64(perfPercentile(all, percentile).Nanoseconds()), fmt.Sprintf("p%d-ns", percentile))
		}
		b.ReportMetric(float64(len(all)), "samples")
	}
}

func perfPercentile(sorted []time.Duration, percentile int) time.Duration {
	return sorted[(len(sorted)*percentile+99)/100-1]
}
