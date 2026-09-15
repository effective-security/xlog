# Roadmap

## Milestone 1 — Correctness and dependable validation

Prerequisite for promoting new ingress buffering.
The list below records the milestone scope; FINDINGS.md tracks current status.

Acceptance: each corrected finding has regression tests, no races during
concurrent logging/reconfiguration/shutdown, observable write errors, closed
owned file handles, executable examples, and CI gates that correctly fail
test/coverage failures. Preserve existing interfaces with additive alternatives.

## Milestone 2 — Measure the synchronous path

The initial local baseline, reproducible benchmark suite, and raw measurements
are in [`Documentation/benchmarks/`](Documentation/benchmarks/README.md).
The suite covers whole-path throughput, message sizes, producer latency,
allocations, synchronous sinks, profiles, and an existing-byte-queue comparison
under bursts/saturation with final drain and gated heap snapshots. Use the
reference workload there as a provisional baseline; target-service arrival-rate
and latency/memory budgets still need validation before choosing ingress defaults.

Add whole-pipeline benchmarks before choosing queue defaults. Existing escaping
benchmarks omit the global lock, formatting, caller capture, and sinks.

Benchmark serial and parallel callers, disabled levels, plain/KV/context/derived
logs, caller on/off, text/JSON/Stackdriver, 0/4/16 fields, and small/2 KiB/64 KiB
records. Compare discard, file, rotation, and controlled slow/failing sinks.
Separate steady state, bursts, saturation, and shutdown under load.

Record producer p50/p95/p99 latency, delivered records/second, allocations/bytes
per record, retained heap, mutex/block profiles, queue occupancy, drops, errors,
and drain duration. Include encoding, enqueueing, and final drain in throughput
accounting so an undrained queue cannot look artificially fast. Report Go
version, hardware, GOMAXPROCS, queue bounds, and run-to-run variation.

Acceptance: a reproducible baseline and explicit producer-latency/memory budgets
for the target workload. Buffering absorbs bursts and moves waiting away from
producers. It also raises sustainable throughput when the destination's cost is
per write rather than per byte, because a record-aware queue can coalesce many
records into one write; see the measured saturation case in
[`Documentation/benchmarks/SINK.md`](Documentation/benchmarks/SINK.md).

## Optional buffered ingress

Implemented as `Sink` plus the `WithSink` formatter option. The earlier
`AsyncFormatter`/`PreparedFormatter` wrapper was removed before release: it
encoded composite JSON values twice, because it snapshotted values on the
producer and assembled the record on the worker.

Now every formatter renders the complete record into a pooled buffer and either
writes it inline or hands it to a sink worker, so there is one encode per record
and the queue carries bytes with known record boundaries. Delivered:
byte budgets, oversize policy, lossy `OverflowDropNewest`, counters through
`SinkStats`, batched destination writes, bounded `FlushWithin`/`CloseWithin`,
a bounded CRITICAL drain through `CriticalFlushTimeout`, and output-lock
elision for concurrent formatters. See
[sink measurements](Documentation/benchmarks/SINK.md) and the README example.

Still future work: context-bounded checked admission, a checked submission API
returning admission errors to callers, cancellation-aware destinations, and
per-level or per-reason drop accounting. `Close` owns only the worker;
applications close their own destinations after draining. A blocked writer can
still block a drain, bounded only by the explicit timeout variants.

### Distinguish the two queue boundaries

| Mode                    | Work on producer                                | Work on worker                           | Current status                                          |
| ----------------------- | ----------------------------------------------- | ---------------------------------------- | ------------------------------------------------------- |
| Synchronous             | Filter, metadata, encode, write                 | None                                     | Default xlog path; rotation can still add a file buffer |
| Buffered bytes          | Filter, metadata, encode, copy/enqueue bytes    | Destination write/flush                  | Existing ChannelWriter; drain/error lifecycle repaired  |
| Buffered record ingress | Filter, metadata, render the whole record once, enqueue bytes | Batch consecutive records, write/flush | Implemented as `Sink` + `WithSink`; bounds records and bytes |

Keep synchronous behavior as the default and a supported mode. First make the
existing byte queue safe; introduce record ingress separately with comparative
benchmarks and explicit delivery semantics. Do not stack both queues by default:
two capacities and flush boundaries obscure memory and loss behavior.

### Proposed boundaries and API

Registry/configuration locking is separate from output serialization, and
sink-backed formatters share the output lock instead of holding it exclusively.
A first-class Record/Encoder split was considered and rejected: the rendered
record is already the owned, immutable representation, and adding a typed
intermediate would reintroduce a second pass over every composite value. One
worker owns every destination buffer, so non-thread-safe byte buffers are never
shared; formatter state that a producer touches is read-only during logging.

The shipped contract keeps `Logger` and `Formatter` source-compatible and adds
one concrete type plus one option:

```go
sink, err := xlog.NewSink(256, xlog.WithQueueBytes(8<<20))
formatter := xlog.NewJSONFormatter(os.Stderr, xlog.WithSink(sink))
```

A `Sink` interface was deliberately not introduced: there is one implementation,
and third-party formatters extend the model by embedding `xlog.Output` rather
than by supplying a sink. Existing void-returning logging calls still need a
separate observable error/status path; a checked submission API for applications
requiring admission feedback remains open.

The legacy Formatter API computes timestamp/caller while formatting, so simply
calling it in a goroutine reports worker metadata. Rendering the whole record on
the producer keeps that metadata correct without a Record/Encoder contract, a
mutable global clock, or a caller-depth hack. Third-party formatters that write
to a destination directly stay synchronous and serialized until they embed
`xlog.Output`.

### Ownership, admission, and ordering

- Capture level decisions, time, and caller at the call site before enqueueing.
  An admitted record keeps the destination and formatting policy selected at
  admission; later configuration updates affect future records.
- Copy field slices and byte payloads. A shallow copy of []any does not own maps,
  pointers, errors, Stringers, or custom marshalers. Normalize/freeze mutable
  values synchronously into owned representations, or require an explicit
  immutable-value contract in a new API. Benchmark the snapshot cost.
- Bound record count and retained bytes, including maximum record size and
  worker batch. Reserve admission capacity before expensive copying; unlimited
  blocked-producer snapshots must not defeat the advertised bound. Define
  oversize-record behavior before accepting it (F-09).
- Start with one FIFO worker. Guarantee admission order and per-producer order;
  concurrent callers have no order until admission. Flush barriers must account
  for rejected/dropped submissions. Avoid synchronous fallback that overtakes
  queued records or accesses an encoder concurrently.
- Full-queue policies: Block (initial conservative default), DropNewest, or
  context-bounded admission through the checked API. Expose dropped/rejected
  counters by level/reason and queue records/bytes/high-water mark. Define
  ERROR/CRITICAL handling explicitly; never silently select a lossy policy.
- Route diagnostics to a separate callback/status mechanism outside locks. It
  must not recursively feed the same sink. Define callback panic handling and
  how observer latency affects callers/workers.

Status: records and retained bytes are bounded, oversize behavior is defined,
Block and DropNewest are both available with counters, one FIFO worker preserves
admission and per-producer order, and admission never holds a lock across
rendering or I/O. Because a record is rendered before its size is known, byte
admission happens after rendering, so a blocked producer holds one buffer
outside the budget. Open: per-level drop accounting and a diagnostics callback.

### Flush, failure, and shutdown

```text
open -> closing (reject new submissions)
     -> drain accepted records -> flush encoder/destination -> close owned sink
     -> closed (publish completion and accumulated error)
```

Flush is an ordered barrier for all records admitted before it, including their
destination writes and buffer flushes. Enqueue success is not delivery success;
neither Flush nor Close implies filesystem durability unless an explicit sync
policy is selected. Report encoding failures, short writes, flush failures, and
close failures; preserve causes using cockroachdb/errors. Define fail-stop versus
continue policy, including how a sticky buffered-write error is handled.

All Close callers observe the same completion; post-close writes return a
closed-sink error. Close owns the worker, timer, buffer, and file writer, while
ownership of caller-supplied writers is explicit. Reconfiguration drains or
retires the old sink safely and never resurrects a closed formatter.

Context cancellation can bound queue admission and the caller's drain wait;
it cannot interrupt an arbitrary blocked io.Writer.Write. Document that limit,
support cancellation-aware/deadline-capable destinations where possible, and
report remaining queued records on timeout. Do not abandon worker goroutines
and imply shutdown completed.

Fatal must submit its final record and use an explicit bounded drain policy
before ExitFunc; deferred Close cannot run after os.Exit. Panic needs an explicit
flush policy too, with tests for recovery by the application. Specify emergency
output behavior when a queue/sink cannot progress, without recursive logging or
concurrent writes to the same unsafe destination.

Status: CRITICAL records, including Fatal and Panic, wait for a bounded barrier
set by `CriticalFlushTimeout`, and `FlushWithin`/`CloseWithin` bound explicit
barriers. Context-aware admission and emergency output remain open.

### Delivery stages and acceptance gates

1. **Safe bytes first:** done. ChannelWriter, error reporting, and rotator
   ownership are fixed, with enqueue/Stop race tests.
2. **Record contract:** done as rendered records. `Output`/`RecordBuffer` give
   record-aware delivery for any formatter, with byte-identical schemas and
   producer timestamps/callers verified by the parity tests.
3. **Opt-in ingress:** done. Bounded queue, policies, stats, and barriers ship
   opt-in; synchronous delivery remains the default and defaults are explicit.
4. **Benchmark and document:** done for throughput, burst latency, saturation,
   shutdown and mutable values; retained-heap measurement of the record queue
   is still outstanding.

Release gate: deterministic tests for full queues, concurrent producers,
Submit/Flush/Close races, repeated Close, delayed/failed/short writes, large
records, mutable inputs, formatter changes, and fatal/panic behavior; race suite
passes, no lost accepted records on successful drain, no goroutine/file leaks,
memory stays within the documented accounting model, and performance meets the
workload budgets from milestone 2. Lossy policies must account for every dropped
record. Keep the feature opt-in until these gates pass.

## Further improvements

- Add checked level configuration and consistent formatter option/default
  behavior; define duplicate/reserved-key and Unicode truncation policy.
- Consider immutable context derivation as an additive API, preserving the
  existing mutable context helper for compatibility.
- Evaluate log/slog integration after Record/Sink contracts stabilize; retain
  xlog's TRACE/DEBUG ordering and ERROR observer semantics explicitly.
- Add native Windows CI before claiming Windows runtime parity. Keep supported
  platform behavior in package docs and the codemap.
- Keep CI on maintained Go 1.27 patch releases and verify tool compatibility.
  The local review used Go 1.27.0. The official [release history](https://go.dev/doc/devel/release)
  and [Go 1.27 notes](https://go.dev/doc/go1.27) are maintenance references.
- Evaluate encoding/json/v2 only as a measured schema change. Go 1.27 continues
  to support encoding/json; stricter v2 defaults require compatibility tests for
  duplicate keys, invalid UTF-8, nil values, and custom marshalers.
