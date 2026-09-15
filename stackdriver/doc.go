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
// options before logging. The current KV serializer can discard records or
// change string types; see FINDINGS.md before using it for production records.
package stackdriver
