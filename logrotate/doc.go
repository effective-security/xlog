// Package logrotate integrates lumberjack file rotation with xlog and provides
// a ChannelWriter for optional background byte writes.
//
// Initialize replaces xlog's global formatter with a PrettyFormatter. For
// example, within an application function returning error:
//
//	closer, err := logrotate.Initialize("./logs", "app", 7, 100, false, os.Stderr)
//	if err != nil {
//		return err
//	}
//	logger := xlog.NewPackageLogger("example.com/app", "main")
//	logger.KV(xlog.INFO, "event", "started")
//	// Stop application producers before closing the logging destination.
//	return closer.Close()
//
// The buffered flag queues already formatted bytes; it does not move xlog
// formatting to a worker. A full queue blocks. Without an extra sink, a file
// buffer is used even with buffered=false. PackageLogger.FlushError and Close
// flush it. Close removes the formatter override, drains, flushes, and closes
// the owned file; concurrent/repeated calls return the same result. Extra sinks
// are flushed when supported but remain caller-owned. Neither Flush nor Close
// guarantees filesystem durability, and stalled destinations can block shutdown.
package logrotate
