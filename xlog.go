// # Copyright 2018, Denis Issoupov
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package xlog

import "context"

// Logger combines structured and printf-style logging.
type Logger interface {
	KeyValueLogger
	StdLogger
}

// KeyValueLogger logs structured records with alternating string keys and values.
type KeyValueLogger interface {
	// KV logs structured entries at level using the configured formatter.
	KV(level LogLevel, entries ...any)

	// ContextKV logs entries in "key1=value1, ..., keyN=valueN" format,
	// and add log entries from ctx as well.
	// ContextWithKV method can be used to add extra values to context
	ContextKV(ctx context.Context, level LogLevel, entries ...any)

	// WithValues derives a logger with additional alternating string keys and values.
	WithValues(keysAndValues ...any) KeyValueLogger
}

// StdLogger provides plain and printf-style logging. PackageLogger fatal methods
// exit and panic methods panic; NilLogger deliberately discards fatal calls.
type StdLogger interface {
	// Fatal logs a critical message and terminates the process for PackageLogger.
	Fatal(args ...any)
	// Fatalf formats a critical message and terminates the process for PackageLogger.
	Fatalf(format string, args ...any)

	// Panic logs a critical message and panics.
	Panic(args ...any)
	// Panicf formats a critical message and panics.
	Panicf(format string, args ...any)

	// Info logs plain entries at INFO.
	Info(entries ...any)
	// Infof logs a printf-style message at INFO.
	Infof(format string, args ...any)

	// Error logs plain entries at ERROR.
	Error(entries ...any)
	// Errorf logs a printf-style message at ERROR.
	Errorf(format string, args ...any)

	// Warning logs plain entries at WARNING.
	Warning(entries ...any)
	// Warningf logs a printf-style message at WARNING.
	Warningf(format string, args ...any)

	// Notice logs plain entries at NOTICE.
	Notice(entries ...any)
	// Noticef logs a printf-style message at NOTICE.
	Noticef(format string, args ...any)

	// Debug logs plain entries at DEBUG, the most verbose threshold.
	Debug(entries ...any)
	// Debugf logs a printf-style message at DEBUG.
	Debugf(format string, args ...any)

	// Trace logs plain entries at TRACE, below DEBUG in verbosity.
	Trace(entries ...any)
	// Tracef logs a printf-style message at TRACE.
	Tracef(format string, args ...any)
}
