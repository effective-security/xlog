# Roadmap

## Milestone 1 — Correctness and dependable validation

Prerequisite for promoting new ingress buffering. F-01 through F-05 and F-08
are now fixed, including error-aware flushing and owned rotation shutdown.
F-07 is partly addressed: configuration and ERROR observers are independent of
output locking; recursive output and enabled-call contention remain. The list
below records the milestone scope; FINDINGS.md tracks current status.

1. Fix Stackdriver serialization (F-01). Choose explicit JSON encodings for large
   integers, errors, enums, and durations; preserve strings as strings.
2. Repair ChannelWriter admission/Stop and rotating-sink ownership (F-02/F-03),
   then expose encoding/write/flush/close errors (F-04). Test all sink/buffer modes.
3. Give derived fields clear ownership and live level state; protect context
   snapshots without silently changing tested context mutation behavior (F-05).
4. Add checked configuration with defined inheritance (F-06), prevent callback
   deadlocks (F-07), and make disabled-output Flush safe (F-08).
5. Repair race/coverage gates and global-state test isolation (F-10). Use
   testing/synctest or explicit channel barriers for worker tests; capture
   subprocess exit behavior for Fatal and environment initialization.

Acceptance: each corrected finding has regression tests, no races during
concurrent logging/reconfiguration/shutdown, observable write errors, closed
owned file handles, executable examples, and CI gates that correctly fail
test/coverage failures. Preserve existing interfaces with additive alternatives.

## Milestone 2 — Measure the synchronous path

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
for the target workload. Buffering mainly absorbs bursts and moves waiting away
from producers; it does not increase the destination's sustainable throughput.

## Optional buffered ingress

### Distinguish the two queue boundaries

| Mode                    | Work on producer                                | Work on worker                           | Current status                                          |
| ----------------------- | ----------------------------------------------- | ---------------------------------------- | ------------------------------------------------------- |
| Synchronous             | Filter, metadata, encode, write                 | None                                     | Default xlog path; rotation can still add a file buffer |
| Buffered bytes          | Filter, metadata, encode, copy/enqueue bytes    | Destination write/flush                  | Existing ChannelWriter; drain/error lifecycle repaired        |
| Buffered record ingress | Filter, metadata, owned field snapshot, enqueue | Encode, batch where allowed, write/flush | Proposed opt-in mode                                    |

Keep synchronous behavior as the default and a supported mode. First make the
existing byte queue safe; introduce record ingress separately with comparative
benchmarks and explicit delivery semantics. Do not stack both queues by default:
two capacities and flush boundaries obscure memory and loss behavior.

### Proposed boundaries and API

Registry/configuration locking is now separate from output serialization. A future Record
contains producer timestamp, level, package, caller PC/file/line, message, and
owned structured fields. An Encoder renders a Record; a Sink admits it and
owns delivery. One worker initially owns each encoder/destination so current
non-thread-safe formatters and byte buffers are never shared across workers.

Illustrative additive contract (names/package placement remain to be settled):

```go
type Sink interface {
    Submit(ctx context.Context, record Record) error
    Flush(ctx context.Context) error
    Close(ctx context.Context) error
}
```

Provide synchronous and bounded buffered implementations behind this contract.
Keep Logger and Formatter interfaces source-compatible. Existing void-returning
logging calls need a separate observable error/status path; offer a checked
submission API for applications requiring admission feedback.

The current Formatter API computes timestamp/caller while formatting, so simply
calling it in a goroutine reports worker metadata. Add record-aware encoders for
built-ins; keep third-party legacy formatters synchronous unless they opt into
the new contract. Do not use a mutable global clock or caller-depth hack to
simulate producer metadata on a worker.

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

### Delivery stages and acceptance gates

1. **Safe bytes first:** fix ChannelWriter, error reporting, and rotator ownership;
   test enqueue/Stop races and every buffering/sink mode.
2. **Record contract:** add record-aware synchronous delivery and adapters;
   verify schema compatibility and correct producer timestamps/callers.
3. **Opt-in ingress:** implement bounded queue, policies, status, and barriers;
   keep the default synchronous and queue defaults explicit.
4. **Benchmark and document:** compare all three modes, including saturation,
   retained memory, shutdown, and unsupported mutable values.

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
