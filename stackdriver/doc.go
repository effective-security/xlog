// Package stackdriver provides xlog's Google Cloud Logging (formerly
// Stackdriver) formatter. It writes local JSON records; it does not upload logs
// or provide a Cloud Logging client.
//
// Example:
//
//	formatter := stackdriver.NewFormatter(os.Stdout, "app")
//	xlog.SetFormatter(formatter)
//	logger := xlog.NewPackageLogger("example.com/app", "main")
//	logger.KV(xlog.INFO, "attempt", 1)
//
// Output fields include logName, component, timestamp, severity, message (an
// object), and sourceLocation. TRACE and DEBUG both map to DEBUG. Configure
// options before logging. Values use JSON encoding: strings retain their type,
// integers remain JSON numbers, durations and display enums become strings,
// and custom JSON marshalers control their representation. The formatter also
// implements xlog.ErrorFormatter to report encoding and destination failures.
package stackdriver
