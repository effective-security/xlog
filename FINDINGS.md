# Findings

Review date: 2026-09-19. Scope: all three library packages, their tests, build
recipes, and CI configuration. Items are open unless explicitly marked fixed.
Severity reflects impact when triggered, not an assigned vulnerability score.
Features and implementation milestones belong in [ROADMAP.md](ROADMAP.md).

## Summary

| ID   | Severity | Status                         | Issue                                                                                |
| ---- | -------- | ------------------------------ | ------------------------------------------------------------------------------------ |
| F-06 | Medium   | Open, reproduced               | Invalid configuration silently disables noncritical logging; defaults do not persist |
| F-07 | High     | Partially fixed                | Global lock permits reentrant deadlocks and serializes all sink latency              |
| F-09 | Medium   | Partially fixed                | Length options do not bound whole records; the sink now bounds queue bytes           |
| F-10 | Medium   | Open, reproduced/source review | CI/race/coverage checks and test isolation have gaps                                 |

## F-06 — Configuration errors and defaults can hide logs

Owner: [logmap.go](logmap.go), `SetRepoLevel`, `NewPackageLogger`, global/repo
setters; [init.go](init.go).

SetRepoLevel discards ParseLevel's error and applies its CRITICAL fallback.
An invalid value was confirmed to make `p.LevelAt(xlog.ERROR)` false. All other
ordinary levels become hidden too. NewPackageLogger always assigns INFO, so
global/repository changes (including XLOG_LEVEL initialization) do not become
defaults for later registrations. Unknown targets are silently ignored.

Validate configuration before applying it. A future checked API should return
errors and reject a whole invalid configuration without partial mutation, then
define inheritance for new registrations and derived loggers explicitly.

## F-07 — Reentrancy can deadlock; sink latency blocks every package

Partially fixed: configuration and output use separate locks. ERROR observers
run outside both locks, even for filtered records; they may query configuration
or log at other levels. Concurrent observers must synchronize their own state,
and recursive ERROR calls need a recursion guard. Tests verify callback reentry,
configuration access from a writer, and filtered/configuration calls completing
while a destination is blocked. Formatter removal waits for admitted calls before
sink closure, without holding the configuration lock.

Further fixed: a formatter backed by a `Sink` reports `Concurrent() true`, so
`PackageLogger` takes the output lock for reading and producers render records
in parallel instead of serializing. Admission takes a short sink mutex over
counters only, never across rendering or I/O. A producer waiting on a full queue
still holds the output lock, but only for reading, so other producers keep
rendering and other packages are no longer blocked behind it. Filtered calls now take no lock at all: the
level is atomic and the ERROR observer is an atomic pointer. The measured effect
with four parallel producers is 2.4x to 3.2x
(see [SINK.md](Documentation/benchmarks/SINK.md)).

Still open: formatters that write to a destination directly, which is every
formatter without a sink, still serialize every enabled call on the exclusive
output lock. Recursive logs from formatters, writers, Stringers, or MarshalJSON
implementations can still deadlock, now on the output lock or inside a
destination write. A blocked destination still blocks producers once its queue
fills. These require an explicit reentrancy policy.

Original evidence (before the partial fix):

Owner: [packagelogger.go](packagelogger.go), `internalLog`/`internalLogf`;
[logmap.go](logmap.go), `loggerStruct` and configuration helpers.

The global mutex remains locked through OnError, caller lookup, rendering,
JSON marshalers, and destination I/O. An OnError callback that calls
`xlog.GetFormatter()` waits for the mutex already held by its caller. A direct
probe hung until interrupted. Logging through xlog or the hijacked standard
logger in callbacks/custom writers has the same problem.

A slow or blocked destination stalls every package, configuration update, and
even disabled log calls waiting to check their level. The existing byte queue
reduces destination wait only until it fills; encoding still runs under this
lock. This contention is established by source structure, not a measured
end-to-end performance regression. Whole-path and sink comparisons now live in
[Documentation/benchmarks/](Documentation/benchmarks/README.md).

Fix direction: separate configuration access from output serialization, move
external callbacks outside registry locks, and establish immutable record
ownership. Preserve formatter safety and output ordering; simply unlocking around
the current shared bufio.Writer is unsafe. See the buffered-ingress roadmap.

## F-09 — Bounds and formatter options are inconsistent

Owners: [options.go](options.go), [formatters.go](formatters.go),
[json_formatter.go](json_formatter.go), [stackdriver/sd.go](stackdriver/sd.go),
[logrotate/channelwriter.go](logrotate/channelwriter.go),
[sink.go](sink.go), [output.go](output.go).

Constructors used to leave MaxLogLength at zero while any Options call activated
2048; see the construction-default fix below.
Text limits only rendered KV values; JSON limits only the plain msg;
Stackdriver ignores that option and uses a separate mutable global. None bounds
total record size, and truncation occurs after allocating/rendering the full
value. Byte slicing can split UTF-8 or text escape sequences. Stackdriver always
computes caller metadata even when caller output is nominally disabled.

ChannelWriter bounds queued item count, not bytes. It allocates/copies before
blocking on admission, so pending producers add memory outside the queue. Its
pool retains arbitrarily large buffers until the runtime chooses to discard
them. Unlike the text encoder pool, it has no retained-capacity cap.

Fixed for the record queue: `Sink` bounds queued records **and** retained record
bytes (`WithQueueBytes`, 8 MiB by default), rejects oversized records under a
defined policy (`WithMaxRecordBytes`), counts oversize and dropped records in
`SinkStats`, and caps pooled record buffers at 64 KiB like the text encoder
pool. A record is rendered before its size is known, so one buffer per blocked
producer sits outside the budget; peak retained memory is the budget, plus one
batch buffer per destination, plus those in-flight buffers.

Also fixed: constructors seed `MaxLogLength` with `DefaultMaxLogMessageLength`
and apply their options, so construction and `Options` no longer disagree. This
is a behavior change: a formatter built without calling `Options` previously had
no limit and now truncates at 2048 bytes.

Still open: per-field and whole-record byte limits, Unicode-safe truncation,
Stackdriver's separate mutable global, and ChannelWriter's unbounded pool.

## F-10 — Validation infrastructure has blind spots

Owners: [.project/gomod-project.mk](.project/gomod-project.mk),
[.github/workflows/unittest.yml](.github/workflows/unittest.yml), existing tests.

- `make covtest RACE=true` selects `-race` with `-covermode=count`; Go requires
  atomic coverage mode for race instrumentation. A direct check reported
  `-covermode must be "atomic", not "count", when -race is enabled`.
  CI does not set RACE=true.
- The coverage helper pipes go test output through grep without pipefail and
  infers failure from the last output line. It does not reliably preserve the
  command's exit status. Use direct exit-status handling or Go test JSON output.
- CI reports coverage with strict `> 80` rather than `>= 80`; the reporting shell
  does not explicitly fail the job on low coverage. Whether the separate status
  blocks merging depends on repository branch protection, which was not inspected.
- The parallel global mutation in `TestInitializeAndClose` and the fixed rotation
  temp path are repaired. Older tests still do not consistently restore globals,
  and original channel tests still poll real time. New concurrency regressions
  use testing/synctest or channel coordination.
- Some assertions do not test their stated behavior: Test_StringFormatter checks
  that a literal contains an empty actual result for a disabled log, which passes
  trivially. Prefer exact empty-output assertions.

These gaps explain why a passing race/coverage run does not clear the open issues.
Repair the gates and add focused regression tests as each finding is fixed.
