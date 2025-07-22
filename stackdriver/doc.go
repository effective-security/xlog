// Package stackdriver implements a Google Cloud Stackdriver formatter for xlog.
// It formats log entries into the JSON structure expected by Stackdriver Logging.
//
// Example:
//
//	formatter := &stackdriver.Formatter{WithTrace: true}
//	logger := xlog.New(os.Stdout, xlog.WithFormatter(formatter))
//	logger.Infof("User logged in")
package stackdriver
