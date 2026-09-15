# Initial performance baseline — 2026-09-15

## Decision

**Synchronous logging can be a bottleneck in services emitting large volumes of
records. An opt-in buffered record-ingress prototype is justified for evaluation.**
The strongest signals are the serialized throughput ceiling, millisecond tail
latency under contention even with discard, and the cost of large text records.
The existing byte queue demonstrably absorbs destination stalls during bursts;
it preserves producer-side encoding and cannot raise sustainable sink throughput.

Keep synchronous logging as the reference/default. These measurements justify
considering the next roadmap stage, not selecting queue defaults or claiming an
unimplemented record queue will meet a production budget.

## Reference workload and environment

Use all three dimensions together: **records/second, message size, and producer
latency**. The general reference is a **64-byte message plus four integer fields,
INFO, timestamp and caller enabled**, text or JSON, with one and eight producers.
That encodes to 174 bytes for text, 202 for JSON, and 281 for Stackdriver in these
fixtures. Typical latency means p50; p95/p99 remain necessary acceptance measures.

Measured on Go 1.27.0, Linux/amd64, GOAMD64=v1, Intel i7-11800H (8 cores / 16
threads), GOMAXPROCS=8. Files used local ext4 on NVMe, through the OS page cache,
without fsync. Source base: `54482ab190caf9ccdceb6f521a3365bfb2e6d385`, with the new
benchmark files and pre-existing documentation edits. No library implementation
changes. See `results/environment.txt` and [methodology](README.md).

Tables use the median of three runs. Throughput runs use 200 ms per benchmark
calibration; latency runs separately use 10,000 calls (1,000 for slow sinks).
They are closed-loop maximum-load experiments, not an independent arrival-rate
test at the suggested service baseline below.

## Typical latency, size and throughput

Text KV, four fields, caller enabled, discard destination:

| Message payload | Serial records/s | Serial p50 | Serial p95 | Serial p99 | Eight-producer records/s | Eight-producer p50 | Eight-producer p99 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 64 B | 449,042 | 2.10 µs | 4.25 µs | 5.94 µs | 393,423 | 2.15 µs | 825 µs |
| 2 KiB | 228,953 | 3.88 µs | 7.23 µs | 15.58 µs | 197,083 | 4.03 µs | 1,075 µs |
| 64 KiB | 10,695 | 71.88 µs | 163.01 µs | 206.13 µs | 10,209 | 1,021.66 µs | 1,465 µs |

Adding producers does not multiply throughput. Typical latency alone hides a
long wait for a minority of callers on small records. On large records,
serialization becomes visible even in the median.

Format comparison for the small reference record, discard destination:
Allocation columns use the serial throughput runs.

| Format | Serial records/s | Eight-producer records/s | Serial p50 | Eight-producer p50 / p95 / p99 | Allocated B/record | Allocs/record |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Text | 449,042 | 393,423 | 2.10 µs | 2.15 / 4.34 / 824.85 µs | 769 | 22 |
| Pretty | 459,299 | 404,428 | Not sampled | Not sampled | 777 | 23 |
| JSON | 243,232 | 220,097 | 3.87 µs | 3.84 / 8.40 / 1,060.31 µs | 1,617 | 37 |
| Stackdriver | 240,234 | 222,524 | 3.81 µs | 3.96 / 10.42 / 1,047.84 µs | 1,449 | 37 |

The serial small-record mean elapsed costs across the three runs were
2.202–2.293 µs for text, 4.032–4.115 µs for JSON, and 4.058–4.185 µs for
Stackdriver. This variation is smaller than the formatter differences. Tail
percentiles are less stable; retain raw samples of repeated runs when comparing
changes. Raw outputs: `results/sync-throughput.txt`, `results/sync-latency.txt`.
Run `python3 Documentation/benchmarks/summarize.py` for medians/min/max of every
reported metric as `results/summary.csv`.

## Destination effects

Same small pretty KV record, synchronous delivery:

| Destination | Serial records/s | Eight-producer records/s |
| --- | ---: | ---: |
| Discard | 463,437 | 412,572 |
| Direct file | 352,105 | 315,592 |
| Rotation, synchronous 8 KiB file buffer | 430,548 | 397,618 |
| Requested 1 ms sleep per write | 930 | 931 |
| Failing writer | 0 | 0 |

Rotation's file buffering reduces write frequency; this is not a rollover or
durability benchmark. Direct-file text latency for the small record was 2.68 µs
p50 / 7.41 µs p99 with one caller, and 2.80 µs p50 / 947.87 µs p99 with eight.
The slow sink dominates both producer latency and delivered throughput.
The failing-sink run asserts the preserved cause and zero delivery; returned
logging calls must never be confused with delivered records.

See `results/sync-sinks.txt` and `results/sync-slow-latency.txt`.

## Where time and contention accumulate

The separate eight-producer small-text profile attributed approximately 44.64 of
44.66 cumulative seconds of mutex wait to `PackageLogger.internalLog`, almost
entirely at its deferred output-mutex release; configuration admission accounted
for only about 5.5 ms. This directly supports output serialization as the
contention source. These are summed waiter durations, not 44 seconds of wall time.
The profiled process ran for about 6.4 seconds, including calibration.

CPU samples include about 28% cumulatively in text field flattening and about
20% in runtime stack-PC collection. These costs are inside producer-side work;
buffering only destination bytes leaves them there. Disabling the caller option
reduced the small text record's serial mean from 2.227 to 1.271 µs, and JSON from
4.111 to 2.729 µs. Stackdriver's option does not disable its function capture.

Filtered KV calls measured about 28 ns serial, 102 ns with eight callers, and
zero allocations. Filtered context logging still copied its fields before the
level decision: 432 B and four allocations per call. See `results/sync-api.txt`.

Profiles: `results/profile-cpu.txt`, `results/profile-mutex.txt`,
`results/profile-output-lock.txt`, `results/profile-block.txt`.

## What the existing byte queue changes

Eight producers, text reference record, 1 ms requested delay per sink write.
All accepted records were delivered; checked flush/close returned no error.
There are no configured drops. The existing queue capacity is 256 chunks, and
each small record was verified to produce one chunk.

| Workload | Mode | Producers finished | Drain after producers | Full delivery time | Delivered records/s | Producer p50 | Producer p99 |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Burst: 64 records | Sync | 69.10 ms | 0.004 ms | 69.10 ms | 926 | 8.58 ms | 9.82 ms |
| Burst: 64 records | Bytes | 0.211 ms | 68.42 ms | 68.66 ms | 932 | 2.47 µs | 184.94 µs |
| Saturation: 2,048 records | Sync | 2,203.28 ms | 0.002 ms | 2,203.28 ms | 930 | 8.60 ms | 8.79 ms |
| Saturation: 2,048 records | Bytes | 1,929.42 ms | 274.29 ms | 2,203.67 ms | 929 | 7.60 ms | 8.77 ms |

Medians are computed independently, so producer and drain columns need not sum
exactly to the median total. The burst producer phase improved about **327×**;
this is **not** a 327× delivery-throughput improvement. Once the queue fills,
producers wait. A worker does not make a 1 ms destination sustain 50k records/s.
The queue also leaves the global output lock and encoding on the producer.

## Allocation and retained memory

At a hypothetical 50k small records/s, the measured per-record allocations imply
about **38 MB/s for text**, **81 MB/s for JSON**, or **72 MB/s for Stackdriver**.
These are allocation rates, not live heap or CPU measurements at that arrival
rate. For 64 KiB messages, text allocates about 444 kB/record, Stackdriver about
296 kB, and JSON about 1.6 kB in this repeated immutable-string fixture. JSON's
reused encoder buffer makes its allocation count unusually insensitive to this
fixture's size; mutable or newly built application payloads are excluded.

In the gated byte-queue experiment, four 64 KiB records retained **262,584 bytes
in 12 undelivered chunks** (one in-flight and eleven queued). The process-wide
post-GC heap delta was 289,856 bytes in all three runs. Four 2 KiB messages had
8,632 pending bytes; four small messages had 696. This demonstrates size-dependent
retention but does not establish a maximum. The queue limits chunks only.

Synchronous small-message heap deltas were negative due to reclaimed runtime/
pool/setup state, so they cannot support a per-record retained-memory claim.
One synchronous 64 KiB call blocked in Write showed a 136,192-byte heap increase;
its state differs from four fully encoded queued records. Use these as snapshots,
not a fair per-record memory comparison or an ingress memory budget.

See `results/load.txt`. The existing byte queue has no public
occupancy/high-water telemetry; this experiment measures pending chunks only
while the destination is gated. No peak queue occupancy was inferred from the
timing runs.

## Provisional general baseline

For evaluating a high-performance service on comparable hardware, start with:

| Dimension | Provisional reference / evaluation gate |
| --- | --- |
| Sustained offered load | 50k records/s, plus bursts; validate with the actual service |
| Record size | 64 B message + four scalar fields; also test 2 KiB and 64 KiB outliers |
| Typical producer latency | p50 ≤ 5 µs for the small record |
| Tail producer latency | p99 ≤ 100 µs at the target offered load |
| Allocation cost | ≤ 2 KiB/record for the small immutable fixture |
| Delivery | No missing records or flush/close errors under a non-failing destination |

These are **proposed evaluation gates**, not measured guarantees at 50k/s. That
rate uses roughly 11% of text's and 21% of JSON's observed serial capacity, leaving
room to investigate headroom. At 100k/s those fractions double. The independent
arrival-rate sweep is still needed to determine when the 100 µs tail gate fails;
the saturated eight-producer runs clearly fail it. With ten logs per request,
50k records/s corresponds to only 5k requests/s.

Do not choose an asynchronous retained-memory budget or queue capacity from these
fixtures. A future prototype needs a record/byte bound including snapshots,
blocked admissions and worker batches, tested against the real size distribution.

## Next step

1. Use the existing byte queue when measurements show short destination stalls
   dominate and its blocking/full-queue semantics are acceptable.
2. Evaluate opt-in record ingress if producer encoding/output-lock latency is the
   limiting factor. Measure snapshot/metadata/enqueue costs against this baseline;
   preserve caller/time and field ownership contracts from M2.
3. Compare at independently scheduled arrival rates around 50k/s and increasing
   fractions of measured capacity. Include bursts, full queues, large/mutable
   values, full drain, errors and concurrent shutdown, with explicit byte budgets.

The baseline and evidence are complete for this local investigation. The full M2
production acceptance gate remains open pending target-service load and memory
budgets; no new ingress implementation is included.

## Validation

- `make test` and `make lint`: passed; lint reported zero issues.
- `GOMAXPROCS=8 XLOG_PERF_LOAD=1 go test -race ./...`: passed, including the load
  and retained-heap experiments.
- Race-instrumented benchmark smoke run (`-benchtime=8x -cpu=8`) of pipeline,
  API, and sink suites: passed.
- All recorded benchmark and load runs passed delivery/error assertions.
