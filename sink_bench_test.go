package xlog_test

import (
	"fmt"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/effective-security/xlog"
	"github.com/effective-security/xlog/logrotate"
	"github.com/stretchr/testify/require"
)

const sinkBurstSize = 32

// BenchmarkSinkPipeline compares complete delivery paths, including the final
// drain. The slow writer models a destination-bound workload; discard exposes
// rendering and queue overhead. Payload construction is excluded in all modes.
func BenchmarkSinkPipeline(b *testing.B) {
	for _, format := range []string{"text", "pretty", "json", "stackdriver"} {
		for _, dest := range []string{"discard", "slow"} {
			for _, size := range []int{perfSmall, perfLarge} {
				for _, parallel := range []bool{false, true} {
					for _, mode := range []string{"sync", "bytes", "sink"} {
						c := pipelineCase{
							format:   format,
							api:      "kv",
							sink:     dest,
							fields:   4,
							size:     size,
							caller:   true,
							parallel: parallel,
						}
						b.Run(fmt.Sprintf("%s/%s", mode, c.name()), func(b *testing.B) {
							benchmarkSink(b, c, mode, false)
						})
					}
				}
			}
		}
	}
}

// BenchmarkSinkBurst samples producer latency in 32-record bursts, flushing
// each burst. Throughput includes all drains; percentiles cover producer calls.
func BenchmarkSinkBurst(b *testing.B) {
	for _, format := range []string{"text", "pretty", "json", "stackdriver"} {
		for _, mode := range []string{"sync", "bytes", "sink"} {
			c := pipelineCase{
				format: format,
				api:    "kv",
				sink:   "slow",
				fields: 4,
				size:   perfSmall,
				caller: true,
			}
			b.Run(fmt.Sprintf("%s/%s", mode, c.name()), func(b *testing.B) {
				benchmarkSink(b, c, mode, true)
			})
		}
	}
}

func benchmarkSink(b *testing.B, c pipelineCase, mode string, sample bool) {
	l := perfGlobals(b)
	w := &perfWriter{dest: io.Discard}
	if c.sink == "slow" {
		w.delay = perfDelay
	}
	var destination io.Writer = w
	var closeWorker func() error
	var options []xlog.FormatterOption
	switch mode {
	case "bytes":
		queue := logrotate.NewChannelWriter(w, perfQueueDepth, 0)
		destination = queue
		closeWorker = queue.Close
	case "sink":
		queue, err := xlog.NewSink(perfQueueDepth)
		require.NoError(b, err)
		options = append(options, xlog.WithSink(queue))
		closeWorker = queue.Close
	}
	formatter := perfFormatter(c.format, destination, c.caller, options...)
	remove := xlog.InstallFormatter(formatter)
	b.Cleanup(func() {
		remove()
		if closeWorker != nil {
			require.NoError(b, closeWorker())
		}
	})
	emit := perfEmit(l, c)
	var samples []int64
	if sample {
		samples = make([]int64, 0, b.N)
	}
	b.ReportAllocs()
	b.ResetTimer()
	if c.parallel {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				emit()
			}
		})
	} else {
		for i := range b.N {
			if sample {
				start := time.Now()
				emit()
				samples = append(samples, time.Since(start).Nanoseconds())
				if (i+1)%sinkBurstSize == 0 {
					require.NoError(b, l.FlushError())
				}
			} else {
				emit()
			}
		}
	}
	drainStart := time.Now()
	err := l.FlushError()
	drain := time.Since(drainStart)
	b.StopTimer()
	require.NoError(b, err)
	require.Equal(b, int64(b.N), w.records)
	b.ReportMetric(float64(w.records)/b.Elapsed().Seconds(), "records/s")
	b.ReportMetric(float64(drain.Nanoseconds()), "drain-ns")
	// Destination writes per record: below 1.0 the worker is batching records.
	b.ReportMetric(float64(w.writes)/float64(w.records), "writes/rec")
	if sample {
		slices.Sort(samples)
		for _, percentile := range []int{50, 95, 99} {
			index := (len(samples) - 1) * percentile / 100
			b.ReportMetric(float64(samples[index]), fmt.Sprintf("p%d-ns", percentile))
		}
	}
}
