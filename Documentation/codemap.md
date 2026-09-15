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
| Global formatter and ERROR observer | `logmap.go` | `SetFormatter`, `GetFormatter`, `OnError`, `OnErrorFn`; `xlog_test.go` |
| Logging, filtering, fatal/panic, attached fields | `packagelogger.go` | `PackageLogger`, `KV`, `ContextKV`, `WithValues`, `Log`, `Logf`, `LevelAt`, `Flush`, `ExitFunc`; `xlog_test.go` |
| Context fields and deletion | `context.go` | `ContextWithKV`, `ContextEntries`; `context_test.go`, `context_entries_test.go` |
| Formatter interface, text, ANSI colors, no-op formatter | `formatters.go` | `Formatter`, `StringFormatter`, `PrettyFormatter`, `NilFormatter`, `NewDefaultFormatter`, `New*Formatter`, `ColorOff`, `LevelColors`; `xlog_test.go`, `helpers_test.go`, `example_test.go` |
| Text values, JSON encoder pool, caller names | `formatters.go` | `EscapedString`, `EscapedInt64`, `EscapedUInt64`, `WithValueString`, `Caller`, `TimeNowFn`; `formatters_test.go`, `helpers_test.go` |
| JSON records and duplicate keys | `json_formatter.go` | `JSONFormatter`, `NewJSONFormatter`, `kvToMap`; `xlog_test.go`, `helpers_test.go`, `example_test.go` |
| Options and value/message limits | `options.go` | `Config`, `FormatterOption`, `Format*`, `DefaultMaxLogMessageLength`; `xlog_test.go`, `helpers_test.go` |
| Environment startup, non-Windows behavior | `init.go` | `init`, `XLOG_LEVEL`, `XLOG_FORMATTER`; no isolated environment subprocess tests yet |
| Standard-library log redirection | `log_hijack.go` | `initHijack`, `packageWriter`, `Stderr`; `log_hijack_test.go` |
| No-op logger and its panic exception | `nillogger.go` | `NilLogger`, `NewNilLogger`; `nillogger_test.go` |
| Rotating files, buffers, destination ownership | `logrotate/logrotate.go` | `Initialize`, internal `logrotator.Close`; `logrotate_test.go`, `init_test.go` in that directory |
| Asynchronous byte writes, backpressure, shutdown | `logrotate/channelwriter.go` | `ChannelWriter`, `NewChannelWriter`, `Write`, `Stop`, `IsStopped`; `channelwriter_test.go` |
| Cloud Logging schema, severity, custom JSON | `stackdriver/sd.go` | `NewFormatter`, `String`, `MaxLogMessageLength`, internal `kventries.MarshalJSON`; `stackdriver/sd_test.go` |
| Build, tools, test and coverage commands | `Makefile`, `.project/gomod-project.mk` | `make test`, `fmt`, `lint`, `generate`, `covtest`, `all` |
| Toolchain/dependencies and CI | `go.mod`, `.golangci.yml`, `.github/workflows/unittest.yml` | Go 1.27, linter v2 config, coverage status, tagging |
| Review findings and future work | `FINDINGS.md`, `ROADMAP.md` | Prioritized defects and staged buffered-ingress proposal |

## Package xlog

### Entry points and global state

`NewPackageLogger(repo, pkg)` registers a single `*PackageLogger` per exact key
pair. New registrations always start at INFO. The registry, formatter, and
ERROR callback share `loggerStruct.Mutex` in `logmap.go`. Repository handles
alias internal maps; they are not independent snapshots.

`SetGlobalLogLevel`, repository setters, and package setters update existing
registered loggers only. `WithValues` returns an unregistered logger with a copied
level and potentially shared slice storage (F-05). `GetRepoLevels` is unordered.
`RepoLogger.SetLogLevel` applies `"*"` first, then explicit packages. Unknown
packages are ignored. `GetRepoLogger` returns an error for an unknown repository;
`MustRepoLogger` panics. `SetRepoLevel(s)` ignores invalid level errors (F-06).

Levels are int8 values: CRITICAL=-1, ERROR=0, WARNING=1, NOTICE=2, INFO=3,
TRACE=4, DEBUG=5. Logging admits `level <= threshold`, with CRITICAL always
admitted. Invalid `LogLevel.Char`/`String` values panic. `ParseLevel` is
case-sensitive; it accepts uppercase names/letters and numbers 0–5, not `-1`.

### Record execution and ownership

```text
PackageLogger.KV / Log / Infof / ...
  -> global logger lock
  -> ERROR observer (even if filtered)
  -> threshold check
  -> field merge and formatting
  -> formatter buffer flush -> destination.Write
  -> unlock
```

`ContextKV` reads/appends context entries before entering this lock. Calls from
all packages serialize through formatting, caller lookup, serialization, and I/O.
Callbacks, custom formatters, writers, Stringers, or MarshalJSON implementations
invoked there must not re-enter xlog or the hijacked standard logger (F-07).
Direct formatter calls bypass this serialization and need caller synchronization.

`SetFormatter` replaces the pointer without flushing or closing the old object.
Nil disables output; `PackageLogger.Flush` nevertheless dereferences it (F-08).
Formatter `Flush` has no error return and only flushes its own `bufio.Writer`.
Built-ins ignore write/flush errors; JSON encoding can silently discard a record
(F-04). Callers own downstream buffer flushing and sink closure.

`Fatal`/`Fatalf` log CRITICAL and invoke `ExitFunc(1)`; default `os.Exit` skips
defers. `Panic`/`Panicf` log CRITICAL then panic. `Log(CRITICAL)` and `KV(CRITICAL)`
do not terminate the process. `NilLogger` discards even fatal calls, but panic
methods call the standard logger and panic. `NilFormatter` only discards output;
it does not suppress `PackageLogger` fatal/panic side effects or ERROR observers.

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
Parents/siblings sharing that state observe updates. `ContextEntries` returns
an internal slice after unlocking: treat it and its referenced values as read-only
(F-05). Built-in output formatters treat a trailing KV key as nil, unlike the
strict context helper; non-string keys panic only if an output formatter sees them.

### Formatter contracts

| Behavior | String / Pretty | JSON | Stackdriver |
| --- | --- | --- | --- |
| Default caller/time | Both enabled | Both enabled | Both enabled |
| Time format | String: UTC RFC3339; Pretty: local time, microseconds | UTC RFC3339 | UTC RFC3339 |
| Plain entries | Individually rendered, separated by space / comma-space | `fmt.Sprint(entries...)` in `msg` | `fmt.Sprint(entries...)` in `message.msg` |
| KV values | Text rendering via `EscapedString` | Native JSON types, detailed error strings | Custom payload serializer; data-loss/type bug F-01 |
| Empty/nil | Omitted unless `PrintEmpty` | Retained regardless of `PrintEmpty` | Nil retained with `PrintEmpty`; empty strings still omitted |
| Duplicate keys | Repeated in output order | Last wins | Repeated object names |
| Caller/location | Options independently control fields | ERROR forces both; otherwise options | Function always emitted/computed; file/line require `WithCaller` and (`WithLocation` or level <= ERROR) |
| Color / skip level | Color only in Pretty; skip level supported in both | Color ignored; skip level supported | Both ignored |
| `MaxLogLength` | Per rendered KV value, then ellipsis; plain entries unlimited | Plain `msg` only; KV unlimited | Ignored; separate `MaxLogMessageLength` limits plain messages |

Constructors leave `MaxLogLength` zero. Any call to `Options`, even with no
arguments, calls `Config.Apply`, which replaces zero with 2048. Negative lengths
disable the text/JSON limits. Truncation counts bytes, not runes or entire records;
it occurs after rendering (F-09). No formatter imposes a total record byte bound.

`EscapedString` is a text-log renderer, **not** a JSON serializer. It trims
strings, selectively quotes them, emits bare time/duration values, prefixes
certain large integers with `_`, and passes `json.RawMessage` through unchanged.
`jsonEncode` intentionally swallows serialization errors and returns an empty
string for unsupported values. Its `sync.Pool` stores a buffer/encoder pair per
call, returns independent strings, and retains buffers only up to 64 KiB.
`stackdriver.String` is a separate JSON encoder without HTML escaping; it also
swallows serialization errors. Current Stackdriver payloads use `EscapedString`
instead, which causes F-01.

`TimeNowFn`, `ExitFunc`, `LevelColors`, `ColorOff`, embedded Config fields, and
Stackdriver's message limit are mutable globals/configuration without concurrent
update protection. Configure before logging and restore hooks in serial tests.

## Package logrotate

Imports xlog and `gopkg.in/natefinch/lumberjack.v2`. `Initialize` creates the
directory (0755), configures `<baseFilename>.log`, captures the global formatter,
and replaces it with a default pretty formatter. `maxAge` is days; `maxSize` uses
lumberjack MB. File opening is lazy. No compression/backup-count options are exposed.

| `buffered` | `extraSink` | Destination chain after formatter |
| --- | --- | --- |
| false | nil | 8 KiB `bufio.Writer` -> lumberjack (no periodic flush) |
| true | nil | 256-item ChannelWriter -> 8 KiB buffer -> lumberjack; 1 s flush tick |
| false | supplied | `io.MultiWriter` -> lumberjack, then extra sink |
| true | supplied | 256-item ChannelWriter -> MultiWriter -> lumberjack, then extra sink |

MultiWriter does not forward `Flush`/`Close` and stops at the first failed sink.
The formatter may emit multiple writes for a large record; queue items are byte
chunks, not necessarily complete records. The queue bounds item count only;
pending producers and pooled buffers can retain additional memory (F-09).

`ChannelWriter.Write` copies input bytes, blocks on a full queue, and reports
enqueue success without downstream error reporting. `Stop` drains and flushes
only for the first caller; `IsStopped` becomes true when shutdown starts. Later
writes have no closed-state check (F-02). The typed `atomic.Bool` tracks admission
to Stop, not a complete lifecycle state machine. Do not copy ChannelWriter.

`logrotator.Close` currently marks closed, flushes the file buffer **before**
stopping the worker, restores the saved formatter, and calls Stop. Repeated Close
returns `already closed`; concurrent Close is unsupported. The file writer is
not retained as a closer, so Close does not close lumberjack (F-03). Producers
must stop first; review F-02/F-03/F-04 before extending this lifecycle.

## Package stackdriver

Imports xlog; no network or Cloud SDK dependency. `NewFormatter(writer, logName)`
returns `xlog.Formatter`, not an exported concrete formatter type. `sd.go` owns
the severity mapping and output schema: `logName`, `component`, `timestamp`,
`message` object, `severity`, `sourceLocation`. TRACE and DEBUG map to DEBUG;
unknown levels map to INFO. See the formatter matrix and F-01 for limitations.

## Tests and verification

Root tests mostly use `package xlog_test`; `helpers_test.go` covers unexported
helpers. `example_test.go` contains executable output examples and TestMain's
fixed clock. Several tests assert exact source lines and compiler-generated
caller names: moving their logging calls requires updating those expectations.
Tests changing globals must not run in parallel and should restore prior values.
`*_extra_test.go` is the preferred layout for additional root coverage.

Logrotate mixes internal channel tests with external rotation tests; current
channel tests poll wall clocks and one initialization test changes global state
in parallel. Stackdriver tests are internal and use exact output/source lines.
There is no testdata directory or generated mock tree. Current benchmarks measure
`EscapedString` only, not logging throughput or sink latency.

Use `make test`, `go test -race ./...`, `make lint`, and `govulncheck ./...`.
`make generate` runs `go generate` and formatting; there are no generated interfaces.
`make all` cleans local artifacts, installs tools, generates/formats, and runs
coverage. `.project/gomod-project.mk` owns shared recipes. CI selects Go via
`go-version-file: go.mod`; its coverage status compares strictly above 80%, and
the workflow does not currently run race checks (F-10). Windows cross-compilation
can be checked on Linux with `GOOS=windows GOARCH=amd64 go test -exec=true ./...`;
that compiles but does not execute Windows tests.
