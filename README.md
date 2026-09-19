# xlog

Per-package structured and printf-style logging for Go 1.27+, derived from
[CoreOS capnslog](https://github.com/coreos/pkg/tree/master/capnslog).
Loggers share one configurable formatter and output destination.

```sh
go get github.com/effective-security/xlog
```

| Package | Purpose |
| --- | --- |
| [`xlog`](doc.go) | Package loggers, levels, context fields, text/color/JSON formatters |
| [`logrotate`](logrotate/doc.go) | Lumberjack rotation and optional background byte writes |
| [`stackdriver`](stackdriver/doc.go) | Local JSON output for Google Cloud Logging |

For contributors and agents, start with [Documentation/codemap.md](Documentation/codemap.md).
[FINDINGS.md](FINDINGS.md) records open bugs and review risks;
[ROADMAP.md](ROADMAP.md) records the work that is still open.

## Quick start

```go
package main

import (
	"os"

	"github.com/effective-security/xlog"
)

var logger = xlog.NewPackageLogger("example.com/app", "main")

func main() {
	xlog.SetFormatter(xlog.NewJSONFormatter(os.Stderr))
	xlog.SetGlobalLogLevel(xlog.INFO)
	logger.KV(xlog.INFO, "event", "started", "version", "v1")
	logger.Infof("listening on port %d", 8080)
}
```

Register library loggers at package scope. Configure formatting and levels in
the application after registration. `NewPackageLogger` returns a concrete
`*PackageLogger`, which implements `Logger`; repeated registration of the same
repository/package returns the same pointer.

By default, no formatter is installed and output is discarded. On non-Windows
platforms, import initialization redirects the standard `log` package through
xlog at INFO, clears its prefix/flags, and reads these variables once:

| Variable | Accepted values | Effect |
| --- | --- | --- |
| `XLOG_FORMATTER` | `DEFAULT`, `PRETTY`, `NIL` (case-insensitive) | Pretty output to stderr, or discarded output; other/unset values install nothing |
| `XLOG_LEVEL` | Level name, letter, or supported number (case-insensitive here) | Updates loggers registered at that moment; invalid values are ignored |

New loggers start at INFO even after an earlier global-level update. Apply levels
in `main`; `XLOG_LEVEL` is not a reliable default for subsequently registered
packages. Windows has no automatic hijacking or environment setup. `xlog.Stderr`
uses the configured formatter on non-Windows and is nil on Windows; it is not a
separate stderr stream.

## Formatting and fields

```go
xlog.SetFormatter(xlog.NewPrettyFormatter(os.Stderr).
	Options(xlog.FormatWithColor(true), xlog.FormatWithCaller(true)))
```

`NewStringFormatter` emits space-separated text; `NewPrettyFormatter` emits
readable text with optional ANSI colors; `NewJSONFormatter` emits one JSON object
per record. `NewDefaultFormatter` selects pretty text. Apply options before
installing the formatter. Direct formatter calls and option mutation are not
safe concurrently; ordinary `PackageLogger` calls are serialized.

`KV` accepts alternating string keys and values. Always supply complete pairs:
built-in output formatters panic on non-string keys, but a trailing key is
treated as nil. Text formatters omit nil and empty strings unless
`FormatPrintEmpty(true)` is enabled; JSON keeps them. JSON uses the last duplicate
key, then writes its own enabled metadata (`time`, `level`, `pkg`, `src`, `func`)
over colliding keys. See the [formatter matrix](Documentation/codemap.md#formatter-contracts)
for length limits and other differences.

```go
ctx := xlog.ContextWithKV(context.Background(), "request_id", "r-123")
logger.ContextKV(ctx, xlog.INFO, "event", "request started")
```

Import `context` for this example. `ContextWithKV` mutates existing shared log
context state and returns the same context once initialized. Nil or empty strings
delete keys. Parent and sibling contexts sharing that state see updates.
`ContextEntries` returns an independent slice snapshot. `WithValues` copies its
persistent fields and follows the registered parent's level changes. Referenced
maps, pointers, and slices are not deeply copied; keep them immutable while
logging. Plain messages on derived loggers use a structured `msg` field.

## Levels

Threshold order is `CRITICAL < ERROR < WARNING < NOTICE < INFO < TRACE < DEBUG`.
A threshold includes itself and lower levels; TRACE enables tracing but excludes
DEBUG. CRITICAL bypasses threshold filtering. `Fatal`/`Fatalf` invoke `ExitFunc(1)`
(normally `os.Exit`); `Panic`/`Panicf` panic. `Log(CRITICAL, ...)` only logs.
`NewNilLogger` discards fatal calls but its panic methods still log and panic.

Within a function returning `error`:

```go
level, err := xlog.ParseLevel("TRACE")
if err != nil {
	return err
}
xlog.SetPackageLogLevel("example.com/app", "worker", level)
```

`ParseLevel` accepts uppercase names/letters and numbers `0` through `5` (ERROR
through DEBUG). `SetGlobalLogLevel`, `SetRepoLogLevel`, and `SetPackageLogLevel`
update already registered loggers. Empty/`"*"` package names select the repository.
Unknown repositories/packages are ignored. Validate `RepoLogLevel.Level` before
calling `SetRepoLevel(s)`: these helpers currently ignore parse errors and can
silence all noncritical logs. Repository `"*"` selects global levels only in those
configuration helpers, not in `SetPackageLogLevel`.

`OnError` observes ERROR calls even when filtered. It runs synchronously without
logger locks and may access configuration or log at other levels. Concurrent
ERROR calls can invoke it concurrently; guard shared callback state and avoid
unguarded recursive ERROR logging. Custom formatters, destination writers, and
value serializers still must not recursively emit through the same output path
(F-07). Configuration reads and changes do not wait for destination I/O.

## Asynchronous logging

Synchronous logging remains the default. A `Sink` moves destination writes onto
one worker and batches them, without changing how records are rendered:

```go
sink, err := xlog.NewSink(256)
if err != nil {
    return err
}
formatter := xlog.NewJSONFormatter(os.Stderr, xlog.WithSink(sink))
remove := xlog.InstallFormatter(formatter)
logger.KV(xlog.INFO, "event", "started")

// Stop application producers before shutdown.
remove()                   // wait for calls admitted through this installation
_ = formatter.FlushError() // ordered delivery barrier
return sink.Close()        // drain, flush, and stop the worker
```

This snippet belongs in a function returning `error`. The `Logger` and
`Formatter` interfaces are unchanged. Every built-in formatter accepts
`WithSink`, including `stackdriver.NewFormatter`; custom formatters opt in by
embedding `xlog.Output`.

### What a sink moves, and what it does not

The producer renders the **complete record** into a pooled buffer before the
logging call returns, and the worker writes it. That single decision sets the
rest of the behavior:

- Producer metadata and caller-owned values are captured before the call
  returns. Custom `Stringer` and `MarshalJSON` implementations run on the
  producer, values may be mutated as soon as the call returns, and **no value is
  encoded twice**. Output is byte-identical to synchronous delivery.
- Encoding stays on the producer, where it scales with cores. A sink-backed
  formatter answers `Concurrent() true`, so `PackageLogger` stops serializing
  output through its global lock. Records are ordered by admission and
  per-goroutine order is preserved; concurrent callers were never ordered.
- Only destination writes move to the worker, which coalesces consecutive
  records for one destination into a single write. Against a destination whose
  cost is per write (a syscall, an RPC) this raises delivered throughput by
  roughly the batch size. Against one whose cost is per byte, it does not.
- A sink does not make a slow destination faster than it is. When the queue
  fills, producers block again, by design.

### Bounds and policies

`NewSink(capacity, opts...)` requires a positive record capacity. Options:

| Option | Default | Purpose |
| --- | --- | --- |
| `WithQueueBytes(n)` | 8 MiB | Bounds retained record bytes, not just records. Non-positive disables the byte bound. |
| `WithMaxRecordBytes(n)` | unlimited | Rejects oversized rendered records, counts them, and reports the rejection through `Err`. |
| `WithBatchBytes(n)` | 64 KiB | Record bytes coalesced into one destination write. The worker also writes whenever the queue drains, so this bounds batching rather than delaying records. |
| `WithFlushInterval(d)` | off | Periodically flushes destinations that support `Flush() error`. |
| `WithOverflow(p)` | `OverflowBlock` | `OverflowBlock` never loses a record; `OverflowDropNewest` discards instead of blocking and counts the drop. |

A record larger than the whole byte budget is queued alone, so a sink without a
record limit cannot deadlock on one large record. Peak retained memory is
approximately the byte budget, plus one batch buffer per destination, plus one
in-flight buffer per producer currently rendering.

`sink.Stats()` reports submitted, written, dropped and oversize counters with
queue occupancy and high-water marks.

### Flush, failure, and shutdown

- `FlushError()` on the formatter, and `Flush()` on the sink, are ordered
  barriers for everything admitted before the call, including destination
  flushes. `FlushWithin(d)` and `CloseWithin(d)` bound the wait so a stalled
  destination cannot block a barrier forever.
- CRITICAL records wait for delivery before fatal or panic side effects, bounded
  by `xlog.CriticalFlushTimeout` (2s by default) so a stuck writer cannot block
  `os.Exit`.
- `Close()` rejects new records, wakes blocked producers, drains, flushes, and
  stops the worker. Concurrent and repeated closes return the same result.
  **A sink that is never closed leaks its worker.** Destinations are flushed
  when supported and never closed.
- Enqueueing is not delivery. `sink.Err()` reports the first write or flush
  failure; the formatter's `Err()` adds its own encoding failures and any
  post-close rejection, wrapped as `io.ErrClosedPipe`. Sticky errors are never
  cleared by later successful records.
- One sink can serve several formatters and destinations. Apply `WithSink` at
  construction; applying it through `Options` rebinds the destination and must
  not race with logging. Avoid stacking a sink over a `ChannelWriter`.

See [measured comparisons](Documentation/benchmarks/SINK.md) for throughput,
burst producer latency, allocation costs, and reproduction commands.

### Custom formatters

A formatter participates in sinks by embedding `xlog.Output` and rendering into
the buffer it hands out, instead of writing to a destination itself:

```go
type myFormatter struct {
    xlog.Config
    xlog.Output
}

func (f *myFormatter) FormatKV(pkg string, l xlog.LogLevel, depth int, entries ...any) {
    record := f.Buffer()
    // ... render the complete record, including its trailing newline ...
    f.Emit(record)
}
```

`Output` supplies `Err`, `Flush`, `FlushError`, `FlushWithin` and `Concurrent`,
and delivers inline or through the sink depending on `Bind`. Use `Discard` when
a record cannot be rendered completely. Formatters that write to a destination
directly keep working unchanged; they are simply serialized as before.

### Application configuration and rotating files

This complete setup helper adds `Async` and `BufSize` to the application flags.
For example, initialize `LogConfig{Async: true, BufSize: 256, LogJSON: true}`;
omitting Async keeps synchronous delivery. The application owns the returned
closer and should check `Close()` errors after stopping producers. Use
`logger.FlushError()` when an explicit delivery barrier is needed during use.

The helper uses Lumberjack directly to select the formatter for the rotating
file. It creates one sink when requested and retains ownership of the file
separately from stderr. `logrotate.Initialize` remains the simpler option when
its installed pretty formatter is sufficient. File opening is lazy, so check
flush/close errors for delivery failures.

```go
import (
    "io"
    "os"
    "path/filepath"
    "sync"

    "github.com/cockroachdb/errors"
    "github.com/effective-security/xlog"
    "github.com/effective-security/xlog/stackdriver"
    "gopkg.in/natefinch/lumberjack.v2"
)

// LogConfig selects the destination, format, and optional delivery worker.
type LogConfig struct {
	LogStd         bool   `help:"also output file logs to stderr"`
	LogDebug       bool   `help:"include source filename and line"`
	LogPretty      bool   `help:"use readable text"`
	LogJSON        bool   `help:"use JSON"`
	LogStackdriver bool   `help:"use Cloud Logging JSON"`
	LogDir         string `help:"log directory; empty uses stderr; /dev/null disables output"`
	Async          bool   `help:"deliver logs on a background worker"`
	BufSize        int    `help:"pending records in the sink; must be positive when Async is true"`
}

type logCloser func() error

func (closeLogs logCloser) Close() error { return closeLogs() }

// Logs installs application logging. Stop producers before closing the result.
// Use logger.FlushError() for an explicit delivery barrier during application use.
func Logs(flags *LogConfig, serviceName string) (io.Closer, error) {
	const maxAgeDays, maxSizeMB = 10, 10
	var destination io.Writer = os.Stderr
	var file *lumberjack.Logger
	if flags.LogDir != "" && flags.LogDir != os.DevNull {
		if err := os.MkdirAll(flags.LogDir, 0755); err != nil {
			return nil, errors.WithMessage(err, "unable to create log directory")
		}
		file = &lumberjack.Logger{
			Filename: filepath.Join(flags.LogDir, serviceName+".log"),
			MaxAge:   maxAgeDays,
			MaxSize:  maxSizeMB,
		}
		destination = file
		if flags.LogStd {
			destination = io.MultiWriter(file, os.Stderr)
		}
	}
	closeFile := func() error {
		if file == nil {
			return nil
		}
		return errors.WithMessage(file.Close(), "unable to close log file")
	}
	// The sink moves destination writes onto one worker and batches them.
	// Records are rendered by the producer, so nothing is encoded twice.
	var sink *xlog.Sink
	options := []xlog.FormatterOption{
		xlog.FormatWithCaller(true),
		xlog.FormatWithLocation(flags.LogDebug),
	}
	if flags.Async {
		var err error
		sink, err = xlog.NewSink(flags.BufSize)
		if err != nil {
			return nil, errors.CombineErrors(err, closeFile())
		}
		options = append(options, xlog.WithSink(sink))
	}
	var formatter xlog.Formatter
	switch {
	case flags.LogDir == os.DevNull:
		formatter = xlog.NewNilFormatter()
	case flags.LogStackdriver:
		formatter = stackdriver.NewFormatter(destination, serviceName, options...)
	case flags.LogJSON:
		formatter = xlog.NewJSONFormatter(destination, options...)
	case flags.LogPretty:
		formatter = xlog.NewPrettyFormatter(destination,
			append(options, xlog.FormatWithColor(file == nil))...)
	default:
		formatter = xlog.NewStringFormatter(destination, options...)
	}
	remove := xlog.InstallFormatter(formatter)
	return logCloser(sync.OnceValue(func() error {
		remove()
		err := xlog.FlushFormatter(formatter)
		if sink != nil {
			err = errors.CombineErrors(err, sink.Close())
		}
		return errors.CombineErrors(err, closeFile())
	})), nil
}
```

The helper and the short sink example are exercised in
[`example_sink_test.go`](example_sink_test.go).

## Rotating files and buffering

Within a function returning `error`:

```go
closer, err := logrotate.Initialize("./logs", "app", 7, 100, false, os.Stderr)
if err != nil {
	return err
}
logger.KV(xlog.INFO, "event", "started")
// Stop application log producers before shutdown.
return closer.Close()
```

Import `github.com/effective-security/xlog/logrotate` for this example. Rotation
uses `app.log`, seven days of retention, and a 100 MB maximum file size.
`Initialize` creates the directory and installs a new pretty formatter; it does
not preserve previous formatter options. File opening is lazy. With an extra
destination, writes reach the file and then that destination sequentially. The
caller owns it. Rotation flushes it when supported but never closes it. This
parameter predates `Sink` and is unrelated to it: it names a second `io.Writer`,
not a delivery worker.

`buffered=true` enables a 256-item `ChannelWriter` queue for **already formatted
bytes**. Formatting and queue admission hold the output lock; a full queue blocks
other enabled logging calls. Configuration and filtered calls remain independent.
Without an extra destination, an 8 KiB file buffer exists even with
`buffered=false`; that path has no periodic flush without the worker.
A `ChannelWriter` queues **bytes** and cannot see record boundaries, so it
cannot coalesce records the way a `Sink` does. Prefer `WithSink` for new code.

`logger.FlushError()` flushes the formatter and supported downstream buffers,
including the byte queue, and reports the first delivery error. `Flush()` performs
the same work but discards the result. CRITICAL records flush before fatal/panic
side effects. Flushing waits for delivery, not filesystem durability; an arbitrary
stalled writer can block it indefinitely.

Rotation `Close` removes its formatter override, waits for admitted calls, drains
queued bytes, flushes, and closes its file. Overlapping rotators can close in any
order; repeated/concurrent closes return the same result. A later `SetFormatter`
supersedes pending overrides. New writes to a stopped ChannelWriter return a
wrapped `io.ErrClosedPipe`; its `Close`, `Flush`, and `Err` expose delivery errors.

### Error reporting

The existing `Formatter` interface remains unchanged. Built-in output formatters
also implement `xlog.ErrorFormatter`: `Err()` safely returns the first observed
encoding/write/flush error, while `FlushError()` flushes supported destinations.
`xlog.FlushFormatter(f)` uses this optional API and supports legacy formatters.
Errors remain available even after successful later records. Unsupported JSON
values reject that record and record the encoding error; text coercion helpers
retain their documented empty-string fallback. Low-level filesystem causes that
lumberjack already converts to strings cannot be recovered by xlog.

Use `xlog.InstallFormatter(f)` when managing a destination lifecycle: its returned
removal function waits for admitted calls and supports out-of-order removal.
`SetFormatter` only replaces configuration; it does not drain or close anything.
Applications close their own sinks and destinations after removing a formatter.
Direct formatter calls are untracked and require caller synchronization.

## Cloud Logging

`stackdriver.NewFormatter(os.Stdout, "app")` installs through `xlog.SetFormatter`.
It writes locally; a collector must forward records. Strings retain their type
and whitespace; integer values remain exact JSON numbers. Durations and display
enums become JSON strings, times use their JSON representation, and custom JSON
marshalers (including RawMessage) are respected. Errors without a JSON marshaler
use detailed text. Empty strings and nil values follow `FormatPrintEmpty`.
Encoding/destination failures are available through `xlog.ErrorFormatter`.
`xlog.NewJSONFormatter` offers JSON output with a different schema.

## Development

The module and CI select Go 1.27 from `go.mod`. The modernization keeps
`encoding/json` semantics; Go 1.27 does not require switching to `encoding/json/v2`.
See the official [Go 1.27 release notes](https://go.dev/doc/go1.27).

```sh
make test
go test -race ./...
make lint
govulncheck ./...
```

`make tools` installs the coverage reporter and pinned linter; `make covtest`
writes coverage artifacts. The current CI coverage status uses a strict `> 80%`
comparison. See F-10 for coverage/CI limitations. No generated mocks or service
entry point exist in this repository.
