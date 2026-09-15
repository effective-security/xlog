package xlog_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/effective-security/xlog"
	"github.com/effective-security/xlog/logrotate"
	"github.com/stretchr/testify/require"
)

const (
	perfBurstRecords      = 64
	perfSaturationRecords = 2048
	perfLoadWorkers       = 8
)

type perfLoadResult struct {
	Mode             string  `json:"mode"`
	Records          int     `json:"records"`
	Workers          int     `json:"workers"`
	QueueChunks      int     `json:"queue_chunks"`
	ProducerMS       float64 `json:"producer_ms"`
	TotalMS          float64 `json:"total_ms"`
	DrainMS          float64 `json:"drain_ms"`
	RecordsPerSecond float64 `json:"records_per_second"`
	P50US            float64 `json:"p50_us"`
	P95US            float64 `json:"p95_us"`
	P99US            float64 `json:"p99_us"`
	Delivered        int64   `json:"delivered"`
	SinkWrites       int64   `json:"sink_writes"`
	AllocatedBytes   uint64  `json:"allocated_bytes"`
	Allocations      uint64  `json:"allocations"`
}

// TestLoggingLoad is an opt-in timing experiment, not a latency assertion in CI.
// Eight closed-loop producers submit a finite burst or enough records to fill
// the existing byte queue. Total throughput includes final flush and shutdown.
func TestLoggingLoad(t *testing.T) {
	if os.Getenv("XLOG_PERF_LOAD") != "1" {
		t.Skip("set XLOG_PERF_LOAD=1 to run the controlled slow-sink experiment")
	}
	for _, count := range []int{perfBurstRecords, perfSaturationRecords} {
		for _, mode := range []string{"sync", "bytes"} {
			t.Run(fmt.Sprintf("%s/records=%d", mode, count), func(t *testing.T) {
				result := perfLoad(t, mode, count)
				data, err := json.Marshal(result)
				require.NoError(t, err)
				t.Log(string(data))
			})
		}
	}
}

func perfLoad(t *testing.T, mode string, count int) perfLoadResult {
	l := perfGlobals(t)
	w := &perfWriter{
		dest:  io.Discard,
		delay: perfDelay,
	}
	var dest io.Writer = w
	var queue *logrotate.ChannelWriter
	if mode == "bytes" {
		queue = logrotate.NewChannelWriter(w, perfQueueDepth, 0)
		dest = queue
		t.Cleanup(func() { require.NoError(t, queue.Close()) })
	}
	formatter := perfFormatter("text", dest, true)
	remove := xlog.InstallFormatter(formatter)
	t.Cleanup(remove)
	emit := perfEmit(l, pipelineCase{
		api:    "kv",
		fields: 4,
		size:   perfSmall,
	})
	samples := make([]time.Duration, count)
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	for worker := 0; worker < perfLoadWorkers; worker++ {
		ready.Add(1)
		done.Go(func() {
			ready.Done()
			<-start
			for i := worker; i < count; i += perfLoadWorkers {
				begin := time.Now()
				emit()
				samples[i] = time.Since(begin)
			}
		})
	}
	ready.Wait()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	begin := time.Now()
	close(start)
	done.Wait()
	producerDone := time.Now()
	// Removal waits for admitted calls; flush and close deliver queued bytes.
	// Shutdown starts after producers finish, avoiding intentional rejection.
	remove()
	err := xlog.FlushFormatter(formatter)
	if queue != nil {
		require.NoError(t, queue.Close())
	}
	end := time.Now()
	runtime.ReadMemStats(&after)
	require.NoError(t, err)
	require.Equal(t, int64(count), w.records)
	require.Equal(t, int64(count), w.writes, "small records must be one queue chunk each")
	slices.Sort(samples)
	result := perfLoadResult{
		Mode:             mode,
		Records:          count,
		Workers:          perfLoadWorkers,
		ProducerMS:       float64(producerDone.Sub(begin)) / float64(time.Millisecond),
		TotalMS:          float64(end.Sub(begin)) / float64(time.Millisecond),
		DrainMS:          float64(end.Sub(producerDone)) / float64(time.Millisecond),
		RecordsPerSecond: float64(count) / end.Sub(begin).Seconds(),
		P50US:            float64(perfPercentile(samples, 50)) / float64(time.Microsecond),
		P95US:            float64(perfPercentile(samples, 95)) / float64(time.Microsecond),
		P99US:            float64(perfPercentile(samples, 99)) / float64(time.Microsecond),
		Delivered:        w.records,
		SinkWrites:       w.writes,
		AllocatedBytes:   after.TotalAlloc - before.TotalAlloc,
		Allocations:      after.Mallocs - before.Mallocs,
	}
	if queue != nil {
		result.QueueChunks = perfQueueDepth
	}
	return result
}

type perfGatedWriter struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *perfGatedWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(p), nil
}

type perfAcceptedWriter struct {
	dest   io.Writer
	chunks int
	bytes  int
}

func (w *perfAcceptedWriter) Write(p []byte) (int, error) {
	n, err := w.dest.Write(p)
	w.chunks++
	w.bytes += n
	return n, err
}

// TestLoggingRetained takes an untimed, post-GC heap snapshot while destination
// progress is gated. It is process-wide evidence, not a per-record memory bound.
func TestLoggingRetained(t *testing.T) {
	if os.Getenv("XLOG_PERF_LOAD") != "1" {
		t.Skip("set XLOG_PERF_LOAD=1 to run the retained-heap experiment")
	}
	for _, size := range []int{perfSmall, perfMedium, perfLarge} {
		for _, mode := range []string{"sync", "bytes"} {
			t.Run(fmt.Sprintf("%s/bytes=%d", mode, size), func(t *testing.T) {
				l := perfGlobals(t)
				gate := &perfGatedWriter{
					entered: make(chan struct{}),
					release: make(chan struct{}),
				}
				var dest io.Writer = gate
				var queue *logrotate.ChannelWriter
				if mode == "bytes" {
					queue = logrotate.NewChannelWriter(gate, perfQueueDepth, 0)
					dest = queue
				}
				accepted := &perfAcceptedWriter{dest: dest}
				formatter := perfFormatter("text", accepted, true)
				remove := xlog.InstallFormatter(formatter)
				t.Cleanup(remove)
				emit := perfEmit(l, pipelineCase{
					api:    "kv",
					fields: 4,
					size:   size,
				})
				runtime.GC()
				var before, held runtime.MemStats
				runtime.ReadMemStats(&before)
				done := make(chan struct{})
				go func() {
					defer close(done)
					if queue == nil {
						emit()
					} else {
						// Four maximum-size records fit within 256 chunks even
						// when a formatter splits each record across many writes.
						for i := 0; i < 4; i++ {
							emit()
						}
					}
				}()
				<-gate.entered
				pendingChunks, pendingBytes := 0, 0
				if queue != nil {
					<-done
					pendingChunks, pendingBytes = accepted.chunks, accepted.bytes
				}
				runtime.GC()
				runtime.ReadMemStats(&held)
				close(gate.release)
				<-done
				require.NoError(t, xlog.FlushFormatter(formatter))
				if queue != nil {
					require.NoError(t, queue.Close())
				}
				t.Logf("mode=%s payload_bytes=%d heap_before=%d heap_held=%d heap_delta=%d pending_chunks=%d pending_bytes=%d",
					mode, size, before.HeapAlloc, held.HeapAlloc,
					int64(held.HeapAlloc)-int64(before.HeapAlloc), pendingChunks, pendingBytes)
			})
		}
	}
}
