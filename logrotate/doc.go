// Package logrotate provides an io.Writer that writes log entries to a file
// and automatically rotates the file when it reaches a specified maximum size.
//
// Example:
//
//	lw, err := logrotate.New("/var/log/app.log", 100*1024*1024)
//	if err != nil {
//	  log.Fatal(err)
//	}
//	logger := xlog.New(lw)
//	logger.Infof("Starting up")
package logrotate
