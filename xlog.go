// Package xlog has slight modifications on the original code,
// adding ability to specify log lever per package,
// and exposing Logger interface, not an implementation structure.
//
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

// Logger interface for generic logger
type Logger interface {
	KeyValueLogger
	StdLogger
}

// KeyValueLogger interface for generic logger
type KeyValueLogger interface {
	// KV logs entries in "key1=value1, ..., keyN=valueN" format
	KV(level LogLevel, entries ...any)

	// ContextKV logs entries in "key1=value1, ..., keyN=valueN" format,
	// and add log entries from ctx as well.
	// ContextWithKV method can be used to add extra values to context
	ContextKV(ctx context.Context, level LogLevel, entries ...any)

	// WithValues adds some key-value pairs of context to a logger.
	// See Info for documentation on how key/value pairs work.
	WithValues(keysAndValues ...any) KeyValueLogger
}

// StdLogger interface for generic logger
type StdLogger interface {
	Fatal(args ...any)
	Fatalf(format string, args ...any)

	Panic(args ...any)
	Panicf(format string, args ...any)

	Info(entries ...any)
	Infof(format string, args ...any)

	Error(entries ...any)
	Errorf(format string, args ...any)

	Warning(entries ...any)
	Warningf(format string, args ...any)

	Notice(entries ...any)
	Noticef(format string, args ...any)

	Debug(entries ...any)
	Debugf(format string, args ...any)

	Trace(entries ...any)
	Tracef(format string, args ...any)
}
