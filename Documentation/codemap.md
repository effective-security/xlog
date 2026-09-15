# Code map for agents

Navigation index for `github.com/effective-security/xlog`, a Go 1.27 logging
library with three packages. No service, `cmd/`, generated mocks, or unrelated
utility packages exist here. Read the owning file and tests before changing it.
Prefer the codebase-memory MCP graph for further discovery; index the repository
if needed. Fall back to text search for missing graph results, literals, or
configuration. Update this map when ownership, entry points, or contracts change.

## Concept index

Paths below are relative to the repository root (one directory above this file).

| Concept | Owning file | Entry points / related tests |
| --- | --- | --- |
| Public logging interfaces | `xlog.go` | `Logger`, `StdLogger`, `KeyValueLogger`; `nillogger_test.go` |
| Registration, levels, repository configuration | `logmap.go` | `NewPackageLogger`, `LogLevel`, `ParseLevel`, `RepoLogger`, `RepoLogLevel`, level setters, `GetRepoLevels`; `logmap_test.go` |
| Global formatter, output locking, ERROR observer | `logmap.go` | `SetFormatter`, `InstallFormatter`, `GetFormatter`, `OnError`, `OnErrorFn`, `ConcurrentFormatter`, internal `acquireOutput`; `xlog_test.go`, `ownership_extra_test.go`, `sink_test.go` |
| Logging, filtering, fatal/panic, attached fields | `packagelogger.go` | `PackageLogger`, `KV`, `ContextKV`, `WithValues`, `Log`, `Logf`, `LevelAt`, `Flush`, `FlushError`, `ExitFunc`, `CriticalFlushTimeout`; `xlog_test.go`, `ownership_extra_test.go` |
| Record delivery worker, queue bounds, batching, lifecycle | `sink.go` | `Sink`, `NewSink`, `SinkOption`, `WithQueueBytes`, `WithMaxRecordBytes`, `WithBatchBytes`, `WithFlushInterval`, `WithOverflow`, `OverflowPolicy`, `SinkStats`, `Flush`, `FlushWithin`, `Close`, `CloseWithin`, `Err`, `Stats`; `sink_test.go` |
| Record rendering buffers and formatter destination side | `output.go` | `Output`, `RecordBuffer`, `Bind`, `Rebind`, `Buffer`, `Emit`, `Discard`, `RecordError`, `Concurrent`; `sink_test.go`, `formatter_errors_extra_test.go` |
| Sink application setup and performance | `example_sink_test.go`, `sink_bench_test.go`, `Documentation/benchmarks/SINK.md` | `ExampleNewSink`, tested README setup helper, `BenchmarkSinkPipeline`, `BenchmarkSinkBurst` |
| Formatter errors and downstream flush | `formatter_errors.go` | `ErrorFormatter`, `FlushFormatter`, `FlushFormatterWithin`; `formatter_errors_extra_test.go` |
| Context fields and deletion | `context.go` | `ContextWithKV`, `ContextEntries`; `context_test.go`, `context_entries_test.go`, `ownership_extra_test.go` |
| Formatter interface, text, ANSI colors, no-op formatter | `formatters.go` | `Formatter`, `StringFormatter`, `PrettyFormatter`, `NilFormatter`, `NewDefaultFormatter`, `New*Formatter`, `ColorOff`, `LevelColors`; `xlog_test.go`, `helpers_test.go`, `example_test.go` |
| Text values, JSON encoder pool, caller names | `formatters.go` | `EscapedString`, `EscapedInt64`, `EscapedUInt64`, `WithValueString`, `Caller`, `TimeNowFn`; `formatters_test.go`, `helpers_test.go` |
| JSON records and duplicate keys | `json_formatter.go` | `JSONFormatter`, `NewJSONFormatter`, `kvToMap`; `xlog_test.go`, `helpers_test.go`, `example_test.go` |
| Options, sink selection, value/message limits | `options.go` | `Config`, `Config.Sink`, `FormatterOption`, `WithSink`, `Format*`, `DefaultMaxLogMessageLength`; `xlog_test.go`, `helpers_test.go` |
| Environment startup, non-Windows behavior | `init.go` | `init`, `XLOG_LEVEL`, `XLOG_FORMATTER`; no isolated environment subprocess tests yet |
| Standard-library log redirection | `log_hijack.go` | `initHijack`, `packageWriter`, `Stderr`; `log_hijack_test.go` |
| No-op logger and its panic exception | `nillogger.go` | `NilLogger`, `NewNilLogger`; `nillogger_test.go` |
| Rotating files, buffers, destination ownership | `logrotate/logrotate.go` | `Initialize`, internal `logrotator.Close`, `rotationSink`; `logrotate_test.go`, `init_test.go`, `lifecycle_extra_test.go`, `ownership_extra_test.go` in that directory |
| Asynchronous byte writes, backpressure, shutdown | `logrotate/channelwriter.go` | `ChannelWriter`, `NewChannelWriter`, `Write`, `Stop`, `Close`, `Flush`, `Err`, `IsStopped`; `channelwriter_test.go`, `channelwriter_extra_test.go`, `lifecycle_extra_test.go`, `ownership_extra_test.go` |
| Cloud Logging schema, severity, custom JSON | `stackdriver/sd.go` | `NewFormatter`, `String`, `MaxLogMessageLength`, internal `kventries.MarshalJSON`; `stackdriver/sd_test.go`, `stackdriver/sd_extra_test.go` |
| Build, tools, test and coverage commands | `Makefile`, `.project/gomod-project.mk` | `make test`, `fmt`, `lint`, `generate`, `covtest`, `all` |
| Toolchain/dependencies and CI | `go.mod`, `.golangci.yml`, `.github/workflows/unittest.yml` | Go 1.27, linter v2 config, coverage status, tagging |
| Review findings and future work | `FINDINGS.md`, `ROADMAP.md` | Prioritized defects and staged buffered-ingress proposal |
| Logging performance baseline, reproduction, generated results | `logging_bench_test.go`, `logging_load_test.go`, `Documentation/benchmarks/` (write-ups and scripts tracked, `results/` ignored) | `BenchmarkSyncPipeline`, `BenchmarkSyncAPI`, `BenchmarkSyncSinks`, `BenchmarkSyncLatency`, shared `perfFormatter`; opt-in `TestLoggingLoad`, `TestLoggingRetained`; whole-path timing, byte-queue comparison, profiles and raw results |

## Package xlog

### Entry points and global state

`NewPackageLogger(repo, pkg)` registers a single `*PackageLogger` per exact key
pair. New registrations always start at INFO. The registry, formatter, and
ERROR callback share `loggerStruct.Mutex` in `logmap.go` for configuration only.
Output has a separate mutex. Repository handles alias internal maps; they are
not independent snapshots.

`SetGlobalLogLevel`, repository setters, and package setters update existing
registered loggers and their derived loggers only. `WithValues` owns a copy of
its field slice and shares the registered parent's synchronized level. Referenced
maps/pointers/slices remain shallow and must not mutate during logging.
`GetRepoLevels` is unordered.
`RepoLogger.SetLogLevel` applies `"*"` first, then explicit packages. Unknown
packages are ignored. `GetRepoLogger` returns an error for an unknown repository;
`MustRepoLogger` panics. `SetRepoLevel(s)` ignores invalid level errors (F-06).

Levels are int8 values: CRITICAL=-1, ERROR=0, WARNING=1, NOTICE=2, INFO=3,
TRACE=4, DEBUG=5. Logging admits `level <= threshold`, with CRITICAL always
admitted. Invalid `LogLevel.Char`/`String` values panic. `ParseLevel` is
case-sensitive; it accepts uppercase names/letters and numbers 0–5, not `-1`.

### Record execution and ownership

The default synchronous path:

```text
PackageLogger.KV / Log / Infof / ...
  -> ERROR observer from an atomic pointer, outside locks (even if filtered)
  -> atomic threshold check; filtered calls take no lock at all
  -> formatter admission under configuration lock
  -> output lock (exclusive, or shared for a ConcurrentFormatter)
  -> field merge -> render the whole record into a pooled buffer
  -> destination.Write, or sink admission and the worker's batched write
  -> unlock -> release formatter admission
```

`ContextKV` starts with an independent context-entry snapshot. Per-call merges
copy persistent fields; plain/printf calls on derived loggers store the message
in a structured `msg` field. Synchronous calls serialize through formatting and I/O, while
configuration access and filtered calls do not wait on the output lock.
Sink-backed and nil formatters share the output lock instead of holding it
exclusively, so their producers render concurrently and never hold it across
queue admission. `PackageLogger.level` is atomic and the ERROR observer is an
atomic pointer, so a filtered call takes no lock.
ERROR observers may run concurrently and may log at other levels or access
configuration. Unguarded recursive ERROR observers recurse indefinitely.
Custom formatters, writers, Stringers, and MarshalJSON implementations must not
recursively emit through the same output path (remaining F-07). Direct formatter
calls bypass output serialization and require caller synchronization.

`SetFormatter` changes configuration without flushing, closing, or waiting for
previously admitted calls. `InstallFormatter` adds a removable override; its
idempotent removal function unlinks it and waits for its admitted calls. Overrides
can be removed in any order and never reinstall a removed destination. A later
SetFormatter supersedes the override chain. Removal must not run from inside the
removed formatter/destination; direct calls and reuse elsewhere are untracked.

Nil disables output and makes Flush/FlushError no-ops. `Formatter` stays source
compatible. Built-in output formatters implement `ErrorFormatter` with concurrent
`Err()` reads and `FlushError()`. They retain the first encoding/write/flush error
for their lifetime; later successful records do not erase evidence of loss.
Unsupported JSON rejects the whole record and records its cause. Text coercion
helpers retain their documented empty-string fallback. Direct destinations are
wrapped to detect short writes before bufio's large-write retry path; existing
bufio.Writer destinations retain the constructor's buffer-reuse semantics.

Ordinary formatting flushes the formatter buffer. Explicit `Flush`, `FlushError`,
and `FlushFormatter` also flush destinations implementing `Flush() error`, including
ChannelWriter barriers. Legacy custom formatters without the optional API can only
be flushed without error reporting. Callers own arbitrary destination closure;
flushing does not imply fsync. A stalled writer can block flushing indefinitely.

CRITICAL records flush supported downstream buffers/queues before returning,
bounded by `CriticalFlushTimeout` (2s) through `FlushFormatterWithin`, so a
stalled destination cannot block `ExitFunc` or a panic indefinitely.
`Fatal`/`Fatalf` then invoke `ExitFunc(1)`; default `os.Exit` skips defers.
`Panic`/`Panicf` log CRITICAL then panic. `Log(CRITICAL)` and `KV(CRITICAL)` do not
terminate the process. `NilLogger` discards even fatal calls, but panic
methods call the standard logger and panic. `NilFormatter` only discards output;
it does not suppress `PackageLogger` fatal/panic side effects or ERROR observers.

### Opt-in record sink

`NewSink(capacity, opts...)` requires a positive pending-record capacity and
starts one FIFO worker. `WithSink(s)` is an ordinary `FormatterOption`, so every
built-in constructor accepts it, including `stackdriver.NewFormatter`. The
application owns `Close`; an unclosed sink leaks its worker. Destinations stay
caller-owned and are flushed when supported, never closed.

Formatters embed `Output` (`output.go`), which owns the destination side:
`Bind` selects inline delivery through an owned `bufio.Writer` or a sink target,
`Buffer` hands out a pooled `RecordBuffer`, `Emit` takes ownership of a rendered
record, `Discard` drops one that could not be rendered, and `RecordError`/`Err`/
`Flush`/`FlushError`/`FlushWithin` supply the `ErrorFormatter` half. Third-party
formatters gain sink support by embedding `Output` instead of writing to a
destination directly; formatters that keep writing directly still work and stay
serialized. `Config.Sink()` exposes the configured sink to formatters outside
this package. `Options` calls `Rebind`, which is configuration, not logging.

Both delivery modes render the complete record, including its trailing newline,
into one pooled buffer and then emit it. There is exactly one encode per record:
producer metadata, custom `Stringer` and `MarshalJSON` implementations, and
validation panics all run on the producer, and queued records hold no reference
to caller-owned values. Sink output is byte-identical to inline output. Buffers
are pooled with the 64 KiB retention cap used by the text encoder pool.

Admission bounds records and retained bytes. `WithQueueBytes` (8 MiB default)
bounds queued record bytes; a record larger than the whole budget is admitted
alone so it cannot deadlock. `WithMaxRecordBytes` rejects oversized records,
counting them in `SinkStats.Oversize` and reporting the rejection through the
formatter's `Err`. `WithOverflow(OverflowDropNewest)` discards instead of
blocking and counts the drop; the default `OverflowBlock` never loses a record.
Admission holds a short mutex over counters only, never across rendering or I/O.
A producer waiting for queue space keeps the output lock for reading, so it does
not block other producers.
Peak retained memory is the byte budget, plus one batch buffer per destination,
plus one in-flight buffer per rendering producer.

The worker owns each target's `bufio.Writer` and is the only goroutine writing
to a destination. It coalesces consecutive records for one target, writing when
`WithBatchBytes` (64 KiB default) is reached or the queue drains, so batching
never delays a record behind an idle queue. Changing target writes the previous
batch first, so only the current target can hold buffered bytes. A panicking
destination is recovered, recorded, and draining continues.

`Flush`/`FlushError` are ordered barriers over everything admitted before the
call, across all targets, followed by destination flushes. `FlushWithin` and
`CloseWithin` bound the wait, including the barrier's own queue admission, so a
stalled destination cannot block them. `Close` marks the sink closed, wakes
blocked producers, closes the queue behind an admission `RWMutex`, drains,
flushes, and publishes one cached result to every caller. Post-close submissions
return a wrapped `io.ErrClosedPipe` that the formatter retains; a rejection is
not a sink delivery failure and never replaces one. `Stats` reports submitted,
written, dropped and oversize counters with queue gauges and high-water marks.

A sink-backed formatter answers `Concurrent() true`, so `PackageLogger` takes
the output lock for reading and producers render in parallel. Records are then
ordered by admission; per-goroutine order is preserved because a producer's next
call cannot be admitted before its previous call returns. `NilFormatter` is also
concurrent. Formatters must not be reconfigured while logging, which is the
existing contract that makes lock-free `Config` reads safe.

Sink lifecycle, parity, ownership, bounds, policies, barriers, timeouts,
concurrency, batching, shared sinks, and fatal/panic are tested in
`sink_test.go`; deterministic worker tests use `testing/synctest` and channels.
`example_sink_test.go` contains the executable example and the tested
application helper copied into README. `sink_bench_test.go` compares
synchronous, byte-queue and sink delivery with the final drain counted, reports
destination writes per record, and separately samples producer latency in bursts.

### Initialization and contexts

Only `init.go` has a `!windows` build constraint. It hijacks the standard `log`
logger, clearing flags/prefix, and registers repository `"log"`, package `""`.
`Stderr` uses the same INFO routing. It reads `XLOG_LEVEL` (uppercased; invalid
ignored) and `XLOG_FORMATTER` once. DEFAULT/PRETTY select pretty stderr output;
NIL discards it; unset/unknown values leave the formatter nil. Later package
registrations still default to INFO. On Windows, initialization is skipped,
`Stderr` stays nil, and explicit formatter setup is required. `NewDefaultFormatter`
is platform-independent in `formatters.go`.

`ContextWithKV` requires even entries and string keys, panicking otherwise.
Once log state exists, it updates the same context/state under a mutex. Nil and
empty strings delete keys; duplicates use the last value; entries sort by key.
Parents/siblings sharing that state observe updates. `ContextEntries` copies
the slice while holding its read lock. Referenced mutable values remain shallow
and must not mutate during logging. Built-in output formatters treat a trailing
KV key as nil, unlike the strict context helper; non-string keys panic only if
an output formatter sees them.

### Formatter contracts

| Behavior | String / Pretty | JSON | Stackdriver |
| --- | --- | --- | --- |
| Default caller/time | Both enabled | Both enabled | Both enabled |
| Time format | String: UTC RFC3339; Pretty: local time, microseconds | UTC RFC3339 | UTC RFC3339 |
| Plain entries | Individually rendered, separated by space / comma-space | `fmt.Sprint(entries...)` in `msg` | `fmt.Sprint(entries...)` in `message.msg` |
| KV values | Text rendering via `EscapedString` | Native JSON types, detailed error strings | JSON types; display values and ordinary errors become strings |
| Empty/nil | Omitted unless `PrintEmpty` | Retained regardless of `PrintEmpty` | Nil/empty strings retained with `PrintEmpty`, otherwise omitted |
| Duplicate keys | Repeated in output order | Last wins | Repeated object names |
| Caller/location | Options independently control fields | ERROR forces both; otherwise options | Function always emitted/computed; file/line require `WithCaller` and (`WithLocation` or level <= ERROR) |
| Color / skip level | Color only in Pretty; skip level supported in both | Color ignored; skip level supported | Both ignored |
| `MaxLogLength` | Per rendered KV value, then ellipsis; plain entries unlimited | Plain `msg` only; KV unlimited | Ignored; separate `MaxLogMessageLength` limits plain messages |

Constructors now seed `MaxLogLength` with `DefaultMaxLogMessageLength` (2048)
and apply their options, so construction and `Options` agree; `Config.Apply`
still replaces a zero value with the same default. Negative lengths disable the
text/JSON limits. Formatters built before this change were unlimited until the
first `Options` call. Truncation counts bytes, not runes or entire records;
it occurs after rendering (F-09). No formatter imposes a total record byte bound.

`EscapedString` is a text-log renderer, **not** a JSON serializer. It trims
strings, selectively quotes them, emits bare time/duration values, prefixes
certain large integers with `_`, and passes `json.RawMessage` through unchanged.
`Caller` resolves one program counter and memoizes the rendered function, file
and line in a package-level `sync.Map` keyed by PC, so repeated call sites skip
the unwind and allocate nothing. `jsonEncode` intentionally swallows
serialization errors and returns an empty string for unsupported values. Its `sync.Pool` stores a buffer/encoder pair per
call, returns independent strings, and retains buffers only up to 64 KiB.
`stackdriver.String` is a separate JSON encoder without HTML escaping; it also
swallows serialization errors. Stackdriver payloads use checked JSON encoding
instead: strings keep type and whitespace, integers keep their exact JSON number,
durations and display enums become strings, times and RawMessage retain their
JSON marshaler representation. Errors without a JSON marshaler use detailed text.

`TimeNowFn`, `ExitFunc`, `LevelColors`, `ColorOff`, embedded Config fields, and
Stackdriver's message limit are mutable globals/configuration without concurrent
update protection. Configure before logging and restore hooks in serial tests.

## Package logrotate

Imports xlog and `gopkg.in/natefinch/lumberjack.v2`. `Initialize` creates the
directory (0755), configures `<baseFilename>.log`, and installs a removable
default pretty formatter through `xlog.InstallFormatter`. `maxAge` is days;
`maxSize` uses lumberjack MB. File opening is lazy. No compression/backup-count
options are exposed.

| `buffered` | `extraSink` | Destination chain after formatter |
| --- | --- | --- |
| false | nil | 8 KiB `bufio.Writer` -> lumberjack (no periodic flush) |
| true | nil | 256-item ChannelWriter -> 8 KiB buffer -> lumberjack; 1 s flush tick |
| false | supplied | `io.MultiWriter` -> lumberjack, then extra sink |
| true | supplied | 256-item ChannelWriter -> MultiWriter -> lumberjack, then extra sink |

The internal rotationSink wraps the destinations above, serializes access,
retains the owned lumberjack closer and the first delivery error, and rejects
writes after closing. It explicitly flushes the file buffer or caller's extra
sink when supported. MultiWriter stops at the first failed sink; it does not
forward Flush/Close itself.
The formatter may emit multiple writes for a large record; queue items are byte
chunks, not necessarily complete records. The queue bounds item count only;
pending producers and pooled buffers can retain additional memory (F-09).

`ChannelWriter.Write` copies input bytes, blocks on a full queue, and reports
enqueue success rather than delivery success. `Err` retains the first destination
write/short-write/flush failure; `Flush` queues a barrier for previously enqueued
bytes and the destination flush. During/after Stop it waits for shutdown and
returns the final error. `Close` combines Stop and Err without closing the
caller-owned destination. `Stop` rejects new writes,
drains accepted writes, and performs the final flush. Every Stop caller waits for
completion. `IsStopped` becomes true when shutdown starts. Rejected writes return
zero and a wrapped `io.ErrClosedPipe`; writes overlapping shutdown may either
enqueue or fail, but successful writes are drained before Stop returns. Queue
closure waits for active senders; blocked senders can exit on the shutdown signal.
A stalled destination can block Stop indefinitely. Do not copy ChannelWriter.

`logrotator.Close` removes its formatter override, waits for admitted logging
calls, flushes the formatter/downstream queue, stops the worker, and closes the
owned file through rotationSink. Downstream flushing never races the worker.
Concurrent and repeated Close calls return the same completion/error result.
Overlapping rotators may close in any order; closing a superseded override never
replaces a newer SetFormatter selection. Extra sinks are flushed, never closed.
Close joins delivery and file-close causes. Lumberjack formats some filesystem
causes into strings before xlog receives them; their concrete types cannot be
recovered. Lazy file-open failures are observable at FlushError/Close.

## Package stackdriver

Imports xlog; no network or Cloud SDK dependency. `NewFormatter(writer, logName)`
returns `xlog.Formatter`, not an exported concrete formatter type. `sd.go` owns
the severity mapping and output schema: `logName`, `component`, `timestamp`,
`message` object, `severity`, `sourceLocation`. TRACE and DEBUG map to DEBUG;
unknown levels map to INFO. The returned formatter implements `xlog.ErrorFormatter`.
See the formatter matrix for output and option differences.

## Tests and verification

Root tests mostly use `package xlog_test`; `helpers_test.go` covers unexported
helpers. `example_test.go` contains executable output examples and TestMain's
fixed clock. Several tests assert exact source lines and compiler-generated
caller names: moving their logging calls requires updating those expectations.
Tests changing globals must not run in parallel and should restore prior values.
`*_extra_test.go` is the preferred layout for additional root coverage.

Logrotate mixes internal channel tests with external rotation tests; current
channel tests poll wall clocks; black-box shutdown regression tests in
`logrotate/channelwriter_extra_test.go` use `testing/synctest` and channels to
verify admission, draining, and concurrent Stop completion. Rotation lifecycle
regressions cover every sink/buffering mode, overlapping and concurrent close,
file ownership, delivery failures, flush barriers, and fatal delivery. Global
formatter tests run serially. Stackdriver's original internal tests use exact
output/source lines; black-box `sd_extra_test.go` decodes JSON types and asserts
encoding/sink failures. Root `ownership_extra_test.go` covers fields, contexts,
level sharing, removable formatters, callback reentry, and blocked destinations.
There is no testdata directory or generated mock tree. Escaping microbenchmarks
remain in `formatters_test.go`. `logging_bench_test.go` adds whole-pipeline
throughput/allocation and separately sampled producer-latency benchmarks, with
serial/parallel callers and checked delivery/flush. `logging_load_test.go` has
opt-in burst, saturation, final-drain and gated retained-heap experiments; enable
them with `XLOG_PERF_LOAD=1`. These root black-box experiments use public logrotate
and Stackdriver APIs, restore the real clock temporarily, and must run serially
with respect to global configuration. `Documentation/benchmarks/run.sh` reproduces
the baseline and separate profiles; its README documents scope and limitations.
Raw benchmark logs, profiles and `summary.csv` are written to
`Documentation/benchmarks/results/`, which is gitignored generated output and can
be deleted at any time. Only the markdown write-ups, `run.sh` and `summarize.py`
are tracked, so every number a document cites must also be quoted in that
document. `sink_bench_test.go` adds the sync/bytes/sink comparison behind
`BenchmarkSinkPipeline` and `BenchmarkSinkBurst`.

Use `make test`, `go test -race ./...`, `make lint`, and `govulncheck ./...`.
`make generate` runs `go generate` and formatting; there are no generated interfaces.
`make all` cleans local artifacts, installs tools, generates/formats, and runs
coverage. `.project/gomod-project.mk` owns shared recipes. CI selects Go via
`go-version-file: go.mod`; its coverage status compares strictly above 80%, and
the workflow does not currently run race checks (F-10). Windows cross-compilation
can be checked on Linux with `GOOS=windows GOARCH=amd64 go test -exec=true ./...`;
that compiles but does not execute Windows tests.
