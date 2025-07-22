// Package xlog provides simple structured logging for Go.
// It supports context-aware logging, multiple formatters (plain text with colors, JSON, Google Stackdriver),
// and log rotation through the logrotate subpackage.
//
// Example:
//
//	logger := xlog.New(os.Stdout, xlog.WithFormatter(xlog.NewJSONFormatter(os.Stdout)))
//	logger.Infof("User %s logged in", userID)
package xlog
