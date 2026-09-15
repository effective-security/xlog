# Sink measurements

Local comparison on 2026-09-19: Go 1.27.0, linux/amd64, Intel Core i7-11800H
(8 cores / 16 threads), GOMAXPROCS=4. Three runs per case. These are short local
measurements, not target-service latency or memory guarantees. Formatting, the
record buffer and the sink are from this change; the synchronous and byte-queue
baselines were rerun alongside them.

All paths count the final flush/drain in ns/op and delivered records/second and
assert exactly N delivered records. Sink construction, payload setup and the
final Close are outside timing; final delivery is inside it. Queue capacity is
256 in both queued modes, with no periodic flush. The message is a 64-byte
string plus four integer fields. Caller capture and the real clock are enabled.
Mode `bytes` uses logrotate.ChannelWriter; mode `sink` uses xlog.NewSink with
xlog.WithSink. No queues are stacked.

`writes/rec` is destination writes per delivered record. Only the sink knows
record boundaries, so only the sink can coalesce records into one write.

## Discard throughput and allocations

500 records per run including the final drain, so the pooled record buffers
reach steady state. The destination is io.Discard behind a counting writer, so
these cases measure rendering, locking and queue handoff, not I/O.

Serial producers:

| Formatter | Mode | ns/record median | B/record | Allocs/record | writes/rec |
| --- | --- | ---: | ---: | ---: | ---: |
| text | sync | 1686 | 526 | 20 | 1.00 |
| text | bytes | 1985 | 580 | 21 | 1.00 |
| text | sink | 1855 | 586 | 20 | 0.03 |
| pretty | sync | 2110 | 534 | 21 | 1.00 |
| pretty | bytes | 2360 | 580 | 22 | 1.00 |
| pretty | sink | 2181 | 585 | 21 | 0.04 |
| json | sync | 4094 | 1374 | 35 | 1.00 |
| json | bytes | 4138 | 1414 | 36 | 1.00 |
| json | sink | 4137 | 1394 | 35 | 0.07 |
| stackdriver | sync | 4078 | 1454 | 37 | 1.00 |
| stackdriver | bytes | 4320 | 1510 | 38 | 1.00 |
| stackdriver | sink | 4358 | 1478 | 37 | 0.07 |

With a destination that costs nothing there is nothing to move off the producer,
so the sink is at best even with synchronous delivery on one producer: it still
renders the whole record, then pays a queue handoff. This is the case that
bounds how much the sink can cost (1% to 10% here), not the case it exists for.

Parallel producers (GOMAXPROCS=4), the shape of a multi-core server:

| Formatter | Mode | ns/record median | B/record | Allocs/record | vs sync |
| --- | --- | ---: | ---: | ---: | ---: |
| text | sync | 1806 | 531 | 20 | 1.0x |
| text | bytes | 2043 | 608 | 21 | 0.9x |
| text | sink | 762 | 943 | 23 | **2.4x** |
| pretty | sync | 2257 | 539 | 21 | 1.0x |
| pretty | bytes | 2504 | 608 | 22 | 0.9x |
| pretty | sink | 919 | 949 | 24 | **2.5x** |
| json | sync | 4040 | 1377 | 35 | 1.0x |
| json | bytes | 4209 | 1429 | 36 | 1.0x |
| json | sink | 1303 | 1666 | 37 | **3.1x** |
| stackdriver | sync | 5143 | 1464 | 37 | 1.0x |
| stackdriver | bytes | 4393 | 1541 | 38 | 1.2x |
| stackdriver | sink | 1602 | 1790 | 39 | **3.2x** |

Synchronous and byte-queue delivery serialize every producer on the output lock,
so extra cores do not help. A sink-backed formatter renders records in parallel
and only the queue handoff is shared. The cost is memory: handing a pooled
buffer to the worker returns it to a different processor's pool, so some
producers allocate a fresh buffer instead of reusing one: roughly 400 extra
bytes per record for text, 300 for JSON.

## Burst producer latency

Three 32-record bursts per run (96 records), with a full flush between bursts.
The writer sleeps 1 ms per write. Percentiles measure only producer calls.
Throughput includes every burst flush.

| Formatter | Mode | Producer p50 (µs) | p95 (µs) | p99 (µs) | Delivered records/s | writes/rec |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| text | sync | 1076.2 | 1104.3 | 1142.5 | 927 | 1.00 |
| text | bytes | 1.70 | 3.45 | 22.32 | 930 | 1.00 |
| text | sink | 1.57 | 2.40 | 16.01 | **27745** | 0.03 |
| pretty | sync | 1079.8 | 1111.7 | 1123.9 | 925 | 1.00 |
| pretty | bytes | 7.06 | 10.75 | 50.33 | 915 | 1.00 |
| pretty | sink | 2.18 | 5.60 | 15.32 | **20665** | 0.04 |
| json | sync | 1079.3 | 1134.8 | 1175.4 | 920 | 1.00 |
| json | bytes | 3.85 | 9.96 | 22.74 | 927 | 1.00 |
| json | sink | 4.10 | 18.38 | 52.45 | **15922** | 0.05 |
| stackdriver | sync | 1083.8 | 1160.2 | 1200.6 | 916 | 1.00 |
| stackdriver | bytes | 4.16 | 10.84 | 37.42 | 931 | 1.00 |
| stackdriver | sink | 3.95 | 7.63 | 21.33 | **14000** | 0.06 |

Both queued modes remove destination waiting from producers. Only the sink also
raises delivered throughput, because it coalesces ~20 records into each write.

## Saturated slow destination

Parallel producers, 1024 records per run (four times queue capacity), 64-byte
message. The queue must apply backpressure; total time includes the drain.

| Formatter | Mode | Delivered records/s median | Final drain (ms) | writes/rec |
| --- | --- | ---: | ---: | ---: |
| text | sync | 925 | 0.00 | 1.00 |
| text | bytes | 921 | 275.16 | 1.00 |
| text | sink | **142629** | 2.18 | 0.006 |
| json | sync | 921 | 0.00 | 1.00 |
| json | bytes | 921 | 275.42 | 1.00 |
| json | sink | **145890** | 2.13 | 0.005 |

**Read this carefully.** The benchmark writer sleeps 1 ms per `Write` call
regardless of size, which models a destination whose cost is per write: a
syscall, an RPC, a rotating file handle. Batching amortizes that cost over every
record in the batch, so throughput rises by roughly the batch size. A
destination whose cost is proportional to bytes written gains nothing from
batching, and no queue raises its sustainable throughput. The earlier claim that
buffering cannot increase destination throughput holds only for the second kind
of destination; it does not hold for per-write costs.

These runs do not report saturated producer percentiles or retained heap.
Queue capacity bounds records; `WithQueueBytes` bounds retained record bytes.

## What changed relative to the removed AsyncFormatter

The previous prepared-entry wrapper snapshotted values on the producer and
assembled the record on the worker, so every composite JSON value was encoded
once by the producer and walked again by the worker. A standalone measurement of
that pattern on one composite field:

```text
BenchmarkSingleEncode        2896 ns/op     713 B/op    26 allocs/op
BenchmarkFreezeThenEncode    3849 ns/op    1345 B/op    32 allocs/op
```

Rendering the whole record once on the producer removes that second pass, and
removes the wrapper's admission mutex and per-record closure with it. Caller
resolution is now memoized per program counter (438 ns / 312 B / 3 allocs, down
to 142 ns / 0 B / 0 allocs per call), which is most of the synchronous text
improvement above.

## Reproduce

Run from the repository root:

```sh
GOMAXPROCS=4 go test -run '^$' \
  -bench 'BenchmarkSinkPipeline/(sync|bytes|sink)/(text|pretty|json|stackdriver)/kv/discard/fields=4/bytes=64/caller=true/parallel=(false|true)$' \
  -benchtime=500x -count=3 -benchmem . > Documentation/benchmarks/results/sink-throughput.txt
GOMAXPROCS=4 go test -run '^$' -bench '^BenchmarkSinkBurst$' \
  -benchtime=96x -count=3 -benchmem . > Documentation/benchmarks/results/sink-burst.txt
GOMAXPROCS=4 go test -run '^$' \
  -bench 'BenchmarkSinkPipeline/(sync|bytes|sink)/(text|json)/kv/slow/fields=4/bytes=64/caller=true/parallel=true$' \
  -benchtime=1024x -count=3 -benchmem . > Documentation/benchmarks/results/sink-saturation.txt
```

Each command writes a raw file including Go's CPU/platform header into
`Documentation/benchmarks/results/`, which is generated output and not tracked.
Rerun the commands to regenerate it; the medians in this file come from that run.
Tests and benchmarks live in `sink_test.go` and `sink_bench_test.go`; the latter
reuses the baseline workload helpers in `logging_bench_test.go`.
