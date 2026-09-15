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
| [`stackdriver`](stackdriver/doc.go) | Local JSON output for Google Cloud Logging; see serialization findings below |

For contributors and agents, start with [Documentation/codemap.md](Documentation/codemap.md).
[FINDINGS.md](FINDINGS.md) records bugs and review risks;
[ROADMAP.md](ROADMAP.md) describes improvements and optional buffered ingestion.

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
delete keys. Parent and sibling contexts sharing that state see updates. Treat
`ContextEntries` as read-only. `WithValues` creates a logger with persistent fields,
but currently shares slice storage in some cases and snapshots its level (F-05).

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

`OnError` observes ERROR calls even when filtered. It executes under the global
lock: callbacks must be fast and must not call xlog, its configuration helpers,
or the hijacked standard logger.

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
sink, writes reach the file and then the extra sink sequentially. The caller
owns and must flush/close the extra sink.

`buffered=true` enables an existing 256-item `ChannelWriter` queue for **already
formatted bytes**. Formatting and queue admission still happen under xlog's global
lock. A full queue blocks every package. With no extra sink, an 8 KiB file buffer
exists even with `buffered=false`; that path has no periodic flush without the
worker. `PackageLogger.Flush` does not drain the worker or guarantee disk durability.

There are open shutdown, resource ownership, and error reporting bugs in this
implementation (F-02/F-03/F-04). Stop all producers before closing; do not write
to a stopped `ChannelWriter`. The [roadmap](ROADMAP.md#optional-buffered-ingress)
defines the proposed lifecycle and record-ingress design.

## Cloud Logging

`stackdriver.NewFormatter(os.Stdout, "app")` installs through `xlog.SetFormatter`.
It writes locally; a collector must forward records. Its current serializer can
silently drop ordinary strings and change their types (F-01). Review that finding
before adopting this formatter. JSON output is also available through
`xlog.NewJSONFormatter`, with a different schema.

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
