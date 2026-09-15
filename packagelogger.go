// Copyright 2015 CoreOS, Inc.
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
	"fmt"
	"os"
)

// ExitFunc terminates the process after Fatal or Fatalf; it defaults to os.Exit.
// Override and restore it only while logging is stopped, for example in tests.
var ExitFunc = os.Exit

// PackageLogger is logger implementation for packages
type PackageLogger struct {
	pkg    string
	level  LogLevel
	values []any
}

const calldepth = 2

type entriesType int

const (
	plain entriesType = iota
	kv
)

// WithValues derives a logger with additional alternating string keys and values.
// It snapshots the current level and may share field storage with other derived
// loggers. See FINDINGS.md before sharing or branching these loggers concurrently.
func (p *PackageLogger) WithValues(keysAndValues ...any) KeyValueLogger {
	return &PackageLogger{
		pkg:    p.pkg,
		level:  p.level,
		values: append(p.values, keysAndValues...),
	}
}

func (p *PackageLogger) internalLog(t entriesType, depth int, inLevel LogLevel, entries ...any) {
	logger.Lock()
	defer logger.Unlock()

	if inLevel == ERROR && logger.onError != nil {
		logger.onError(p.pkg)
	}

	if inLevel != CRITICAL && p.level < inLevel {
		return
	}
	if len(p.values) > 0 {
		entries = append(p.values, entries...)
	}
	if logger.formatter != nil {
		if t == plain {
			logger.formatter.Format(p.pkg, inLevel, depth+1, entries...)
		} else {
			logger.formatter.FormatKV(p.pkg, inLevel, depth+1, entries...)
		}
	}
}

func (p *PackageLogger) internalLogf(depth int, inLevel LogLevel, format string, args ...any) {
	logger.Lock()
	defer logger.Unlock()

	if inLevel == ERROR && logger.onError != nil {
		logger.onError(p.pkg)
	}

	if inLevel != CRITICAL && p.level < inLevel {
		return
	}
	if logger.formatter != nil {
		entries := []any{fmt.Sprintf(format, args...)}
		if len(p.values) > 0 {
			cfg := Config{
				PrintEmpty: false,
			}
			entries = append(cfg.flatten(p.values...), entries)
		}

		logger.formatter.Format(p.pkg, inLevel, depth+1, entries...)
	}
}

// LevelAt reports whether the logger's threshold includes l.
func (p *PackageLogger) LevelAt(l LogLevel) bool {
	logger.Lock()
	defer logger.Unlock()
	return p.level >= l
}

// Logf logs a printf-style message at l, including CRITICAL and DEBUG.
// CRITICAL bypasses filtering but does not itself exit or panic.
func (p *PackageLogger) Logf(l LogLevel, format string, args ...any) {
	p.internalLogf(calldepth, l, format, args...)
}

// Log logs plain entries at l, including CRITICAL and DEBUG.
// CRITICAL bypasses filtering but does not itself exit or panic.
func (p *PackageLogger) Log(l LogLevel, args ...any) {
	p.internalLog(plain, calldepth, l, args...)
}

// Panic and fatal

// Panicf logs a printf-style message at CRITICAL, then panics with that message.
func (p *PackageLogger) Panicf(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	p.internalLog(plain, calldepth, CRITICAL, s)
	panic(s)
}

// Panic logs fmt.Sprint(args...) at CRITICAL, then panics with that message.
func (p *PackageLogger) Panic(args ...any) {
	s := fmt.Sprint(args...)
	p.internalLog(plain, calldepth, CRITICAL, s)
	panic(s)
}

// Fatalf logs a printf-style message at CRITICAL, then calls ExitFunc(1).
func (p *PackageLogger) Fatalf(format string, args ...any) {
	p.internalLogf(calldepth, CRITICAL, format, args...)
	ExitFunc(1)
}

// Fatal logs fmt.Sprint(args...) at CRITICAL, then calls ExitFunc(1).
func (p *PackageLogger) Fatal(args ...any) {
	s := fmt.Sprint(args...)
	p.internalLog(plain, calldepth, CRITICAL, s)
	ExitFunc(1)
}

// Error Functions

// Errorf logs a printf-style message at ERROR.
func (p *PackageLogger) Errorf(format string, args ...any) {
	p.internalLogf(calldepth, ERROR, format, args...)
}

// Error logs plain entries at ERROR.
func (p *PackageLogger) Error(entries ...any) {
	p.internalLog(plain, calldepth, ERROR, entries...)
}

// Warning Functions

// Warningf logs a printf-style message at WARNING.
func (p *PackageLogger) Warningf(format string, args ...any) {
	p.internalLogf(calldepth, WARNING, format, args...)
}

// Warning logs plain entries at WARNING.
func (p *PackageLogger) Warning(entries ...any) {
	p.internalLog(plain, calldepth, WARNING, entries...)
}

// Notice Functions

// Noticef logs a printf-style message at NOTICE.
func (p *PackageLogger) Noticef(format string, args ...any) {
	p.internalLogf(calldepth, NOTICE, format, args...)
}

// Notice logs plain entries at NOTICE.
func (p *PackageLogger) Notice(entries ...any) {
	p.internalLog(plain, calldepth, NOTICE, entries...)
}

// Info Functions

// Infof logs a printf-style message at INFO.
func (p *PackageLogger) Infof(format string, args ...any) {
	p.internalLogf(calldepth, INFO, format, args...)
}

// Info logs plain entries at INFO.
func (p *PackageLogger) Info(entries ...any) {
	p.internalLog(plain, calldepth, INFO, entries...)
}

// KV logs the provided key/value pairs at the given level.
// The entries must come in pairs of key (string) and value;
// built-in output formatters panic on non-string keys. A trailing key is
// treated as having a nil value. Filtered entries are not validated.
func (p *PackageLogger) KV(l LogLevel, entries ...any) {
	p.internalLog(kv, calldepth, l, entries...)
}

// ContextKV logs the provided key/value pairs at the given level,
// prefixed by any entries stored in the context via ContextWithKV.
// The entries must come in pairs of key (string) and value;
// built-in output formatters panic on non-string keys. A trailing key is
// treated as having a nil value. Filtered entries are not validated.
func (p *PackageLogger) ContextKV(ctx context.Context, l LogLevel, entries ...any) {
	extra := ContextEntries(ctx)
	if len(extra) > 0 {
		entries = append(extra, entries...)
	}
	p.internalLog(kv, calldepth, l, entries...)
}

// Debug Functions

// Debugf logs a printf-style message at DEBUG.
func (p *PackageLogger) Debugf(format string, args ...any) {
	p.internalLogf(calldepth, DEBUG, format, args...)
}

// Debug logs plain entries at DEBUG.
func (p *PackageLogger) Debug(entries ...any) {
	p.internalLog(plain, calldepth, DEBUG, entries...)
}

// Trace Functions

// Tracef logs a printf-style message at TRACE.
func (p *PackageLogger) Tracef(format string, args ...any) {
	p.internalLogf(calldepth, TRACE, format, args...)
}

// Trace logs plain entries at TRACE.
func (p *PackageLogger) Trace(entries ...any) {
	p.internalLog(plain, calldepth, TRACE, entries...)
}

// Flush flushes the current global formatter. It panics if no formatter is set
// and does not drain a ChannelWriter or guarantee durable storage.
func (p *PackageLogger) Flush() {
	logger.Lock()
	defer logger.Unlock()
	logger.formatter.Flush()
}
