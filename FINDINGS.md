# Findings

Review date: 2026-09-15. Scope: all three library packages, their tests, build
recipes, and CI configuration. Items are open unless explicitly marked fixed.
Severity reflects impact when triggered, not an assigned vulnerability score.
Features and implementation milestones belong in [ROADMAP.md](ROADMAP.md).

## Summary

| ID   | Severity | Status                         | Issue                                                                                  |
| ---- | -------- | ------------------------------ | -------------------------------------------------------------------------------------- |
| F-01 | High     | Fixed                          | Stackdriver drops ordinary values and changes string types                             |
| F-02 | High     | Fixed                          | ChannelWriter accepts writes after shutdown; later writes can hang                     |
| F-03 | High     | Fixed                          | Rotator flush races its worker and never closes the file writer                        |
| F-04 | High     | Fixed                          | Serialization and sink errors are lost; Close can falsely report success               |
| F-05 | High     | Fixed                          | Derived loggers share fields and retain stale levels; context exposes internal storage |
| F-06 | Medium   | Open, reproduced               | Invalid configuration silently disables noncritical logging; defaults do not persist   |
| F-07 | High     | Partially fixed                | Global lock permits reentrant deadlocks and serializes all sink latency                |
| F-08 | Medium   | Fixed                          | Flush panics when logging is disabled or unconfigured                                  |
| F-09 | Medium   | Open, source review            | Length options do not bound memory, queue bytes, or total record size                  |
| F-10 | Medium   | Open, reproduced/source review | CI/race/coverage checks and test isolation have gaps                                   |
| F-11 | Medium   | Fixed                          | Windows build excluded a required exported constructor                                 |

## F-01 — Stackdriver silently loses records or changes their meaning (fixed)

Fixed: payload values now use JSON serialization rather than text escaping.
Strings retain type/whitespace, integers retain exact JSON numeric spelling,
durations/display enums become strings, and JSON marshalers keep control of
times, RawMessage, and custom representations. Errors without JSON marshalers
use detailed text. PrintEmpty now retains empty strings as well as nil.
Unsupported values reject the record and are observable through ErrorFormatter.
Black-box tests cover these representations and encoding/write/short-write errors.

Original evidence (before the fix):

Owner: [stackdriver/sd.go](stackdriver/sd.go), `kventries.MarshalJSON` and
`formatter.format`; [formatters.go](formatters.go), `EscapedString`.

The custom payload marshaler inserts text-log representations as raw JSON.
`EscapedString("hello")` returns bare `hello`, which is invalid JSON. The outer
`json.Marshal` fails and its caller skips the entire record without reporting it.
Bare durations, times, underscore-prefixed large integers, and some errors have
the same problem. A string such as `"true"` becomes a JSON boolean instead.

Reproduced with `NewFormatter(&buffer, "review").FormatKV("probe", xlog.INFO,
0, "value", value)`:

| Value                      | Observed result                    |
| -------------------------- | ---------------------------------- |
| `"hello"`                  | No output                          |
| `"hello world"`            | Record written with a string value |
| `"true"`                   | Record written with boolean `true` |
| `time.Second`              | No output                          |
| `uint64(9007199254740991)` | No output                          |

This also affects plain messages because they become a `msg` KV field. Existing
tests mostly use spaced strings or numeric fields, which miss this boundary.
Fix direction: encode values with a JSON serializer, define large-number/error
representation, and surface serialization failure. Add type-preserving tests
for strings, enums, durations, times, empty values, RawMessage, and unsupported values.

## F-02 — ChannelWriter has no closed-write admission check (fixed)

Owner: [logrotate/channelwriter.go](logrotate/channelwriter.go), `Write`, `Stop`,
and `listen`.

Before the fix, Write still sent to the open queue after Stop terminated the worker.
For a depth-one queue, `cw.Stop(); cw.Write([]byte("lost"))` returned `(4, nil)`
with `IsStopped()==true`; no worker remained to deliver those bytes. The next
write blocked indefinitely. Depth zero blocked on the first post-stop write.
Concurrent Write/Stop could likewise leave accepted writes behind after draining.

Previously, only the first Stop caller waited; concurrent subsequent callers
could return before drain completion.

Fixed by synchronizing queue closure with active senders, broadcasting shutdown
to blocked writers, draining the closed queue, and broadcasting completion after
the final flush. New writes after shutdown begins return zero and a wrapped
`io.ErrClosedPipe`. Overlapping writes may enqueue or fail; every accepted write
is drained before any Stop caller returns. IsStopped continues to report shutdown
initiation, not completion. A stalled arbitrary io.Writer remains non-cancellable,
and destination errors remain tracked separately in F-04.

Regression tests in `logrotate/channelwriter_extra_test.go` use `testing/synctest`
and channel coordination for zero/depth-one queues, blocked producers, concurrent
Stop callers, final-flush completion, repeated Stop, post-stop writes (including
empty writes), and concurrent write/shutdown delivery accounting.
Validation: `make test`, `go test -race ./logrotate -count=20`, and `make lint`
(zero issues) passed.

## F-03 — Rotation shutdown ordering and file ownership are incomplete (fixed)

Fixed: rotation now installs a removable formatter override that supports
out-of-order removal and waits for admitted logger calls. Shutdown flushes and
drains before closing the retained lumberjack writer. The destination wrapper
serializes buffer access, rejects late writes, and flushes caller-owned extra
sinks without closing them. Concurrent/repeated Close calls now wait and return
the same result (the previous repeated-close error contract is replaced).
Tests cover all four modes, overlap, concurrent close, file ownership, lazy-open
and sink errors, post-close writes, explicit flushing, and fatal delivery.

Original evidence (before the fix):

Owner: [logrotate/logrotate.go](logrotate/logrotate.go), `Initialize` and `Close`.

With `buffered=true` and no extra sink, Close calls `fileBuf.Flush` while the
ChannelWriter worker may be writing or flushing that same bufio.Writer. Producers
being stopped is insufficient: queued writes or timer flushes can still run.
The source permits a race on buffer state and possible corruption; the existing
race suite does not exercise this combination during active draining.

The lumberjack instance is stored only behind io.Writer/bufio/MultiWriter. Close
never calls its Close method. Open file descriptors can remain live after the
application believes it has closed rotation. The `closed` bool and formatter
restoration also have no support for concurrent closes or overlapping rotators;
closing out of order can reinstall an obsolete destination.

Fix direction: establish one owner for downstream writes and final flush, stop
admission, drain, flush, close the owned file writer, then complete shutdown with
errors. Restore the prior formatter through a coordinated handoff. Do not close
caller-owned extra sinks implicitly. Add tests for all four buffering/sink modes,
overlap, concurrent close, failed writes, and file-handle release.

## F-04 — Errors and downstream flush guarantees disappear (fixed)

Fixed through additive APIs: built-in output formatters implement ErrorFormatter
(Err and FlushError), PackageLogger adds FlushError, and FlushFormatter supports
legacy formatters. The first encoding/write/flush error remains observable even
after successful later records. ChannelWriter adds Err, Flush barriers, and Close;
short writes become wrapped io.ErrShortWrite. Rotation Close joins delivery and
file-close errors, preserving causes it receives. Lumberjack itself formats some
filesystem causes into strings, so those underlying concrete types remain lost.

Explicit flushing reaches supported downstream buffers and queues. CRITICAL
records flush before fatal/panic side effects. Flush is delivery, not fsync;
arbitrary stalled writers can block indefinitely. Legacy void APIs still discard
the result, and text coercion helpers intentionally retain their documented
fallback. Normal records retain buffering; unbuffered rotation has no timer,
so applications needing immediate visibility use FlushError.
Tests inject encoding/write/short-write/flush/close failures and verify causes,
queue barriers, and fatal delivery before ExitFunc.

Original evidence (before the fix):

Owners: [formatters.go](formatters.go), [json_formatter.go](json_formatter.go),
[stackdriver/sd.go](stackdriver/sd.go), and [logrotate](logrotate/logrotate.go).

Built-in formatters discard encoding/write/flush errors. A JSON KV field holding
an unsupported value (for example a channel) can discard a complete record.
The text helper `jsonEncode` and `stackdriver.String` instead coerce encoding
failures to empty strings. These existing coercions are now documented.

ChannelWriter discards destination Write/Flush errors and short writes. Rotation
Close already returns error but ignores file-buffer Flush errors. Initialize
checks directory creation only; lazy file opening can fail later without any
error visible to the caller. A bufio.Writer retains a write error, so subsequent
logs can disappear after the initial failure.

With no extra sink and `buffered=false`, the file buffer has no timed flush:
low-volume records can remain invisible until it fills or Close runs.
With an extra sink, MultiWriter neither flushes that sink nor forwards Flush to
the ChannelWriter ticker. `PackageLogger.Flush` is not a queue barrier or fsync.
Fatal exits can therefore lose the fatal record in a downstream queue/buffer.

Fix direction: add an error-reporting sink/lifecycle API without changing the
existing Formatter interface in place, make Close preserve causes with
cockroachdb/errors, and document enqueue versus delivery versus durability.

## F-05 — Derived fields, levels, and context ownership are unsafe (fixed)

Fixed: derived and per-call field slices have independent backing storage.
Derived loggers reference their registered parent's level under the configuration
lock, so package/repository/global updates apply immediately to future calls.
ContextEntries returns a slice snapshot without changing ContextWithKV's shared
parent/sibling update behavior. Plain and printf calls on derived loggers keep
persistent fields structured and store the message in msg, without extra brackets.
Snapshots are shallow; referenced mutable values must remain immutable during
logging. Regression tests cover branching, per-call overwrite, input mutation,
live levels, concurrent configuration/derivation, and context mutation isolation.

Original evidence (before the fix):

Owners: [packagelogger.go](packagelogger.go), `WithValues`, `internalLog`,
`internalLogf`, `ContextKV`; [context.go](context.go), `ContextEntries`.

WithValues appends to the parent's slice without cloning it; siblings share
backing storage whenever capacity permits. Reproduced:

```go
base := p.WithValues("a", 1, "b", 2, "c", 3).WithValues("base", 1)
one := base.WithValues("child", "one")
_ = base.WithValues("child", "two")
one.KV(xlog.INFO, "event", "check") // child was "two" in the output
```

The per-call append in internalLog can also overwrite a child's persistent
fields. WithValues reads `p.level` outside the registry lock and creates an
unregistered copy: later parent/repository/global changes never update it.
After setting the parent to CRITICAL, a derived INFO logger still emitted INFO.
Concurrent derivation/configuration can race. This can misattribute fields or
leave verbose logging enabled after an operator attempts to disable it.

ContextEntries returns its internal slice. Changing a returned value from 1 to
99 changed the next ContextEntries result without updating the internal map.
Concurrent caller mutation can race with logging. ContextKV appends before the
logger lock; any spare backing capacity can be shared. ContextWithKV's mutable
parent/sibling behavior is already asserted by tests and is a compatibility
constraint, not a change to make silently.

Also, internalLogf appends its `entries` slice as a single value instead of
expanding it (`append(..., entries)`), producing bracketed formatted messages
for derived loggers. Ordinary plain methods concatenate persistent fields as
plain entries instead of preserving structured key/value interpretation.

Fix direction: explicitly own field slices, share synchronized level state with
the registered logger, and decide whether context APIs return snapshots. Keep
deep ownership of maps/pointers explicit, especially before asynchronous ingress.

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

Still open: output is synchronous and serialized. An enabled recursive log from
a formatter, writer, Stringer, or MarshalJSON implementation can wait on the
output lock held by its outer call. Slow sinks still block other enabled logging
calls. These require record/encoder/sink boundaries and an explicit reentrancy
policy; the current change does not claim to fix them.

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
end-to-end performance regression. Current benchmarks exercise only value escaping.

Fix direction: separate configuration access from output serialization, move
external callbacks outside registry locks, and establish immutable record
ownership. Preserve formatter safety and output ordering; simply unlocking around
the current shared bufio.Writer is unsafe. See the buffered-ingress roadmap.

## F-08 — Flush dereferences a nil formatter (fixed)

Fixed: Flush and the new FlushError return normally when the global formatter
is nil. The formatter lifecycle regression covers disabled output and restoration
to nil. The same guard handles unconfigured startup state.

Original evidence (before the fix):

Owner: [packagelogger.go](packagelogger.go), `PackageLogger.Flush`.

Logging explicitly supports a nil global formatter and starts that way when no
environment formatter is selected, but Flush calls its method unconditionally.
`xlog.SetFormatter(nil); p.Flush()` panics. Add a nil guard and tests for both
startup and explicitly disabled output; define downstream flush separately (F-04).

## F-09 — Bounds and formatter options are inconsistent

Owners: [options.go](options.go), [formatters.go](formatters.go),
[json_formatter.go](json_formatter.go), [stackdriver/sd.go](stackdriver/sd.go),
[logrotate/channelwriter.go](logrotate/channelwriter.go).

Constructors leave MaxLogLength at zero, while any Options call activates 2048
by default. Text limits only rendered KV values; JSON limits only the plain msg;
Stackdriver ignores that option and uses a separate mutable global. None bounds
total record size, and truncation occurs after allocating/rendering the full
value. Byte slicing can split UTF-8 or text escape sequences. Stackdriver always
computes caller metadata even when caller output is nominally disabled.

ChannelWriter bounds queued item count, not bytes. It allocates/copies before
blocking on admission, so pending producers add memory outside the queue. Its
pool retains arbitrarily large buffers until the runtime chooses to discard
them. Unlike the text encoder pool, it has no retained-capacity cap.

Define consistent construction defaults, field/message/record limits, Unicode
semantics, and byte-budget admission before copying. Measure complete pipelines
before making allocation or throughput claims.

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

## F-11 — Windows constructor availability (fixed)

`NewDefaultFormatter` was defined in `init.go`, excluded by `!windows`, while
logrotate and root helper tests referenced it unconditionally. Windows amd64
cross-compilation failed with `undefined: xlog.NewDefaultFormatter` and
`undefined: NewDefaultFormatter`. The constructor now lives in platform-neutral
`formatters.go`. Linux tests and Windows cross-compilation pass. Windows test
binaries were not executed; automatic non-Windows initialization stays scoped
as before and is now documented.

## Review evidence and limits

The original suite passed on Go 1.27.0 linux/amd64, including `-race`, at 92.8%
total statement coverage. Targeted standalone probes exposed F-01, F-02, F-05,
F-06 and F-07 despite that pass; their inputs/results are recorded above.
Source-reviewed race/resource/error paths are labeled separately and need focused
regression tests, not assumptions based on aggregate coverage.

`govulncheck ./...` reported no known vulnerabilities on the review date. That
checks published vulnerability data, not the logic defects above. No production
load test or Cloud collector integration test was performed.

After modernization, the race suite with coverage passed at 92.6% total
statement coverage. Linux tests, Windows amd64 cross-compilation, `make lint`
(zero issues), and `git diff --check` passed.
`go fix -diff ./...` reported no remaining modernizer changes. These checks
validate this change; they do not resolve the open findings.

## High-severity repair validation

F-01, F-03, F-04, and F-05 are fixed, along with F-08. F-07 remains partially
fixed: observer/configuration deadlocks are repaired, while recursive output and
enabled-call contention remain open. The existing F-02 fix is retained and its
queue now supports error reporting and explicit flush barriers.

`make test`, the full race suite with atomic coverage (94.1% total statements),
20 repeated race-enabled runs of rotation/queue regressions, Windows amd64
cross-compilation, `make lint` (zero issues), and `git diff --check` passed.
Windows binaries were compiled, not executed. The checks do not resolve F-06,
the remaining F-07 behavior, F-09, or the remaining CI/test-isolation work in F-10.
