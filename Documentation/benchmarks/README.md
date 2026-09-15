# Synchronous logging performance

See [measured results and the buffering recommendation](RESULTS.md) for the
initial baseline. This document describes how to reproduce and interpret it.

## Reproduce

Run from the repository root with Go selected by `go.mod`:

```sh
mkdir -p /tmp/xlog-bench-tmp
TMPDIR=/tmp/xlog-bench-tmp bash Documentation/benchmarks/run.sh
```

The script writes raw Go benchmark output, load experiments, environment details,
and text profiles to `Documentation/benchmarks/results/`. That directory is
generated output: it is gitignored and safe to delete at any time. Only the
markdown write-ups and the two scripts are tracked, so every number quoted in
them has to be readable without the raw logs. `XLOG_BENCH_OUT` selects another
output directory. It takes several minutes. Run on an otherwise idle machine; do not run tests, builds, or
other benchmarks concurrently. File benchmarks use `testing.B.TempDir()` beneath
`TMPDIR`: on this host `/tmp` is tmpfs, so the measured run explicitly uses an ext4
directory on `/mnt/shared`. File writes reach the OS page cache, without `fsync`.

For a short reference workload:

```sh
go test -run '^$' -bench '^BenchmarkSyncPipeline$/text/kv/discard/fields=4$/bytes=64$/caller=true$' -benchmem -benchtime=1s -count=5 -cpu=8 .
```

## What is measured

`logging_bench_test.go` uses real `PackageLogger` calls through filtering,
configuration locking, output serialization, metadata, encoding, writes, and a
final checked flush. Each run checks the number of delivered newline-terminated
records. Fixtures contain no literal newlines. A lightweight destination wrapper
counts successful bytes and newline records; this adds some overhead, including
a byte scan for large records. Discard is an encoding/locking baseline, not a
model of a production destination.

| Suite                   | Coverage                                                                                                                                               |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `BenchmarkSyncPipeline` | Text, pretty, JSON, Stackdriver; caller option on/off; 0/4/16 additional integer fields; 64 B/2 KiB/64 KiB message; serial/parallel; discard           |
| `BenchmarkSyncAPI`      | Plain, printf, KV, context, derived, filtered KV, filtered context; small messages; serial/parallel                                                    |
| `BenchmarkSyncSinks`    | Same small pretty KV record through discard, file, synchronous rotation, slow, failing destinations                                                    |
| `BenchmarkSyncLatency`  | Text/JSON/Stackdriver KV, four fields, all sizes, discard/file/slow, serial/parallel; separate latency instrumentation                                 |
| `TestLoggingLoad`       | Eight producers; 64-record burst and 2,048-record saturation; synchronous vs existing 256-chunk byte queue; 1 ms requested sleep per destination write |
| `TestLoggingRetained`   | Post-GC heap with destination gated; one blocked synchronous record vs four admitted byte-buffered records, at all three sizes                         |

`bytes=` describes the message payload, not encoded record size. `fields=` counts
additional fields beside `msg`; plain/printf have no structured fields. Context
and derived loggers hold the four fields persistently, while KV passes them on
each call. Setup, immutable fixture creation, logger derivation, and context
construction are outside the timer. Per-call field merging/copying is inside it.
The root test clock is restored to `time.Now` during experiments. Timestamps are
enabled. Truncation is disabled/raised so the large fixtures really reach the
destination. Stackdriver still captures the function with its caller option off;
the option is not a genuine metadata-off switch for that formatter.

Parallel runs use exactly `GOMAXPROCS` producers sharing one logger/destination.
Serial runs use one producer even with `-cpu=8`. Each producer has a fixed share
of records, avoiding a shared benchmark counter in the hot loop. These are
closed-loop saturation measurements: callers immediately submit their next log
after returning. They do not model independent scheduled arrivals, request
processing, or a particular service's offered load. Queuing outside the logging
call is not represented in latency percentiles.

`ns/op` is total elapsed time divided by attempted records, **not** parallel call
latency. `records/s` counts successful delivery including final flush. Disabled
and failing cases correctly deliver zero. A failing destination rejects its
first write; the formatter retains its sticky buffer error and later calls do
not imply new successful destination writes. Its low call time is not useful
throughput. Rotation measures `Initialize(..., false, nil)` with its 8 KiB file
buffer and checked final flush, not rollover/backup costs. Short runs stay below
its 1 GiB rollover threshold; very long runs will fail the record-count check if
they roll over. Flush does not imply durability.

Latency is measured in a separate suite with at most 4,096 evenly spaced samples
per worker and nearest-rank percentiles. Timing includes the logging call and
the ending clock read; storage and sorting are outside the sampled duration.
Use uninstrumented suites for allocations/throughput. Normal latency runs use
10,000 calls; slow-sink runs use 1,000 calls for the 64 B payload. Tail estimates
from these finite samples are descriptive, not latency guarantees. Sleep requests
are 1 ms; actual wake-up time depends on scheduling and is reflected in results.

The burst/saturation experiment includes encoding, queue copying, admission,
destination writes, final flush, and worker close in total throughput. Producer
completion and drain are reported separately. Shutdown begins after all
producers finish; shutdown racing submissions is covered by lifecycle correctness
tests, not this timing experiment. Every run asserts complete delivery and one
chunk per small record. These modes block instead of dropping when full; there
were no configured lossy policies. Queue capacity is chunks, not a byte bound.

The retained-heap experiment pauses the first destination write with channels,
then forces GC before reading process-wide heap. In the byte case, all four
records have been enqueued and none delivered; `pending_chunks` includes the
worker's in-flight chunk, and `pending_bytes` counts copied, undelivered bytes.
In the synchronous case one producer is blocked inside the first write, with
no asynchronous queue. Heap deltas include runtime/encoder/pool noise and may be
negative for tiny payloads. They are not comparable per-record memory limits.
Allocation counters in the timing experiment include process activity during
the measured interval; use microbenchmark `B/op` for the usual steady-state cost.

Profiles are captured in a separate run because instrumentation changes timing.
Mutex delay is cumulative waiting across goroutines, not elapsed wall time;
block profiles also include expected benchmark/channel/wait-group coordination.

## Interpretation limits

This is a local baseline, not a production service capacity promise. Target
hardware, request rate, logs per request, sink behavior, data types and record
size distribution all matter. The matrix varies the main dimensions without
testing every API × formatter × destination combination. No new record-ingress
queue, batching encoder, overload policy, or queue default is selected here.
An independent arrival-rate sweep, production sink measurements, concurrent-close
timings and byte-bounded queue memory guarantees remain future acceptance work.

## Sink comparison

See [SINK.md](SINK.md) for the record-sink comparison, complete-drain
throughput, burst latency, saturation measurements, raw data, and reproduction.
