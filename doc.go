// Package xlog provides per-package structured and printf-style logging.
// Register loggers in libraries and configure the global formatter in main:
//
//	logger := xlog.NewPackageLogger("example.com/app", "main")
//	xlog.SetFormatter(xlog.NewJSONFormatter(os.Stderr))
//	logger.KV(xlog.INFO, "event", "started", "version", "v1")
//
// Logging is synchronous by default and serialized across packages, including
// formatting and destination writes. NewSink plus the WithSink formatter option
// opt into a bounded record queue and one delivery worker: the producer renders
// the complete record, the worker batches and writes it, and no value is
// encoded twice. Sink-backed formatters are safe for concurrent use, so they
// are not serialized on the output lock. Applications own the sink's Close.
// Producer metadata and mutable values are captured before logging returns in
// both modes. Configuration uses a separate lock. Built-in output formatters
// expose ErrorFormatter; PackageLogger.FlushError reports delivery failures and
// flushes supported downstream queues/buffers. The default threshold is INFO;
// DEBUG is more verbose than TRACE. No output formatter is installed unless
// explicitly configured or selected by XLOG_FORMATTER on non-Windows platforms.
//
// On non-Windows platforms, importing xlog redirects the standard log package
// through xlog at INFO and reads XLOG_LEVEL and XLOG_FORMATTER once. Stderr uses
// that same routing. ContextWithKV mutates existing shared log context state.
//
// The logrotate subpackage adds file rotation and optional queued byte writes;
// its "extra sink" parameter is a second io.Writer, not a Sink.
// The stackdriver subpackage formats Cloud Logging records. See FINDINGS.md for
// remaining configuration, reentrancy, and size-limit issues, and Documentation/codemap.md
// for formatter contracts, ownership, and navigation.
package xlog
