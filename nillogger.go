// Copyright 2018, Denis Issoupov
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package xlog

import (
	"context"
	"log"
)

// NilLogger does not produce any output
type NilLogger struct {
}

// NewNilLogger creates new nil logger
func NewNilLogger() Logger {
	return &NilLogger{}
}

// Fatal does nothing
func (l *NilLogger) Fatal(args ...any) {}

// Fatalf does nothing
func (l *NilLogger) Fatalf(format string, args ...any) {}

// Fatalln does nothing
func (l *NilLogger) Fatalln(args ...any) {}

// Panic does nothing
func (l *NilLogger) Panic(args ...any) {
	log.Panic(args...)
}

// Panicf does nothing
func (l *NilLogger) Panicf(format string, args ...any) {
	log.Panicf(format, args...)
}

// Info does nothing
func (l *NilLogger) Info(entries ...any) {}

// Infof does nothing
func (l *NilLogger) Infof(format string, args ...any) {}

// KV does nothing
func (l *NilLogger) KV(_ LogLevel, entries ...any) {}

// ContextKV logs entries in "key1=value1, ..., keyN=valueN" format,
// and add log entries from ctx as well.
// ContextWithKV method can be used to add extra values to context
func (l *NilLogger) ContextKV(_ context.Context, _ LogLevel, _ ...any) {}

// Error does nothing
func (l *NilLogger) Error(entries ...any) {}

// Errorf does nothing
func (l *NilLogger) Errorf(format string, args ...any) {}

// Warning does nothing
func (l *NilLogger) Warning(entries ...any) {}

// Warningf does nothing
func (l *NilLogger) Warningf(format string, args ...any) {}

// Notice does nothing
func (l *NilLogger) Notice(entries ...any) {}

// Noticef does nothing
func (l *NilLogger) Noticef(format string, args ...any) {}

// Debug does nothing
func (l *NilLogger) Debug(entries ...any) {}

// Debugf does nothing
func (l *NilLogger) Debugf(format string, args ...any) {}

// Trace does nothing
func (l *NilLogger) Trace(entries ...any) {}

// Tracef does nothing
func (l *NilLogger) Tracef(format string, args ...any) {}

// WithValues adds some key-value pairs of context to a logger.
// See Info for documentation on how key/value pairs work.
func (l *NilLogger) WithValues(keysAndValues ...any) KeyValueLogger {
	return l
}
