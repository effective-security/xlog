// Package xlog provides per-package structured and printf-style logging.
// Register loggers in libraries and configure the global formatter in main:
//
//	logger := xlog.NewPackageLogger("example.com/app", "main")
//	xlog.SetFormatter(xlog.NewJSONFormatter(os.Stderr))
//	logger.KV(xlog.INFO, "event", "started", "version", "v1")
//
// Logging is synchronous and serialized across packages, including formatting
// and destination writes. Configuration uses a separate lock. Built-in output
// formatters expose ErrorFormatter; PackageLogger.FlushError reports delivery
// failures and flushes supported downstream queues/buffers. The default threshold is INFO; DEBUG is more verbose
// than TRACE. No output formatter is installed unless explicitly configured or
// selected by XLOG_FORMATTER on non-Windows platforms.
//
// On non-Windows platforms, importing xlog redirects the standard log package
// through xlog at INFO and reads XLOG_LEVEL and XLOG_FORMATTER once. Stderr uses
// that same routing. ContextWithKV mutates existing shared log context state.
//
// The logrotate subpackage adds file rotation and optional queued byte writes.
// The stackdriver subpackage formats Cloud Logging records. See FINDINGS.md for
// remaining configuration, reentrancy, and size-limit issues, and Documentation/codemap.md
// for formatter contracts, ownership, and navigation.
package xlog
