# Roadmap

Open work only. Shipped behavior is documented in [README.md](README.md) and
[`Documentation/codemap.md`](Documentation/codemap.md), measurements in
[`Documentation/benchmarks/`](Documentation/benchmarks/README.md), and open
defects in [FINDINGS.md](FINDINGS.md). Completed milestones live in git history.

Standing constraints for everything below: keep synchronous delivery the default
and a supported mode, keep `Logger` and `Formatter` source-compatible, prefer
additive APIs, and never silently select a lossy policy.

## Workload budgets

The local baseline, the reproducible suite, and the sync/bytes/sink comparison
exist. What is still missing is the target service's own numbers.

- Validate the target service's arrival rate and its producer-latency and memory
  budgets, then choose queue capacity, byte budget and batch defaults from them
  rather than from the reference workload.
- Measure retained heap for the record queue under saturation. The existing
  retained-heap experiment covers ChannelWriter only, so the documented sink
  accounting model (byte budget, plus one batch buffer per destination, plus one
  in-flight buffer per rendering producer) is reasoned, not measured.

## Record sink follow-ups

- Offer a checked submission API. Void logging calls cannot report admission
  failures, so applications that need feedback have no path other than polling
  `Err` or `Stats`.
- Support context-bounded admission and cancellation-aware destinations for
  ordinary records. CRITICAL admission is already bounded by
  `CriticalFlushTimeout`; everything else waits indefinitely under
  `OverflowBlock`. A context can bound queue admission and a caller's drain
  wait; it cannot interrupt a blocked `io.Writer.Write`. Document that limit, report remaining
  queued records on timeout, and never abandon a worker goroutine while implying
  shutdown completed.
- Account for dropped records by level and reason, not just as a total, and
  define ERROR/CRITICAL handling under `OverflowDropNewest` explicitly.
- Route diagnostics to a callback outside locks that cannot recursively feed the
  same sink. Define callback panic handling and how observer latency affects
  producers and the worker.
- Specify emergency output when a sink cannot progress, without recursive
  logging or concurrent writes to the same unsafe destination.
- Byte admission happens after rendering, because a record's size is not known
  before it is rendered. A blocked producer therefore holds one buffer outside
  the budget. Consider reserving a count slot before rendering if the residual
  matters for a real workload.
- Do not stack a sink over a `ChannelWriter`: two capacities and two flush
  boundaries obscure memory and loss behavior. Consider migrating
  `logrotate.Initialize` to a sink so there is one queue with record boundaries.

## Formatter, configuration, and API

- Add checked level configuration that rejects an invalid configuration whole,
  and define inheritance for new registrations and derived loggers (F-06).
- Define duplicate/reserved-key policy and Unicode-safe truncation, and give
  fields and whole records real limits (F-09).
- Consider immutable context derivation as an additive API, preserving the
  existing mutable context helper for compatibility.
- Evaluate `log/slog` integration now that the sink contract is stable; retain
  xlog's TRACE/DEBUG ordering and ERROR observer semantics explicitly.

## Platform and toolchain

- Add native Windows CI before claiming Windows runtime parity. Keep supported
  platform behavior in package docs and the codemap.
- Repair the coverage and race gates described in F-10 before treating a green
  run as evidence.
- Keep CI on maintained Go 1.27 patch releases and verify tool compatibility.
  The local review used Go 1.27.0. The official [release history](https://go.dev/doc/devel/release)
  and [Go 1.27 notes](https://go.dev/doc/go1.27) are maintenance references.
- Evaluate encoding/json/v2 only as a measured schema change. Go 1.27 continues
  to support encoding/json; stricter v2 defaults require compatibility tests for
  duplicate keys, invalid UTF-8, nil values, and custom marshalers.
