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
	"slices"
	"sync/atomic"
	"time"
)

// ExitFunc terminates the process after Fatal or Fatalf; it defaults to os.Exit.
// Override and restore it only while logging is stopped, for example in tests.
var ExitFunc = os.Exit

// CriticalFlushTimeout bounds both halves of a CRITICAL record's wait: queue
// admission when a sink is full, and the delivery barrier before Fatal calls
// ExitFunc or Panic panics. A stalled destination therefore delays process exit
// by at most twice this value instead of blocking it. Zero or negative waits
// forever. A record dropped at admission is counted in SinkStats.Dropped and
// reported through the formatter's Err. A Write already blocked in the
// destination cannot be interrupted, and records still queued on timeout stay
// queued.
var CriticalFlushTimeout = 2 * time.Second

// PackageLogger is logger implementation for packages
type PackageLogger struct {
	pkg string
	// level is read on every call, including filtered ones, so it is atomic
	// rather than guarded by the configuration lock.
	level  atomic.Int32
	values []any
	parent *PackageLogger // registered logger that owns the level
}

const calldepth = 2

type entriesType int

const (
	plain entriesType = iota
	kv
)

// WithValues derives a logger with additional alternating string keys and values.
// The field slice is copied; referenced maps, pointers, and slices are not deeply
// copied and must not be mutated concurrently with logging. Derived loggers share
// the registered parent's level, including later configuration changes.
func (p *PackageLogger) WithValues(keysAndValues ...any) KeyValueLogger {
	parent := p
	if p.parent != nil {
		parent = p.parent
	}
	return &PackageLogger{
		pkg:    p.pkg,
		parent: parent,
		values: append(slices.Clone(p.values), keysAndValues...),
	}
}

// prepare invokes observers outside locks and admits output under the registry
// lock, so removing a formatter can wait for all calls that selected it.
// Filtered calls take no lock at all.
func (p *PackageLogger) prepare(inLevel LogLevel) *formatterRegistration {
	if inLevel == ERROR {
		if observer := currentOnError(); observer != nil {
			observer(p.pkg)
		}
	}
	if inLevel != CRITICAL && p.currentLevel() < inLevel {
		return nil
	}
	logger.Lock()
	defer logger.Unlock()
	if logger.formatter == nil {
		return nil
	}
	if logger.current == nil {
		logger.current = &formatterRegistration{formatter: logger.formatter}
	}
	registration := logger.current
	registration.active.Add(1)
	return registration
}

// currentLevel reads the effective threshold without taking a lock.
func (p *PackageLogger) currentLevel() LogLevel {
	if p.parent != nil {
		return LogLevel(p.parent.level.Load())
	}
	return LogLevel(p.level.Load())
}

func (p *PackageLogger) internalLog(t entriesType, depth int, inLevel LogLevel, entries ...any) {
	registration := p.prepare(inLevel)
	if registration == nil {
		return
	}
	defer registration.active.Done()
	defer releaseOutput(acquireOutput(registration.formatter))
	if len(p.values) > 0 {
		if t == plain {
			entries = []any{"msg", fmt.Sprint(entries...)}
			t = kv
		}
		entries = append(slices.Clone(p.values), entries...)
	}
	if t == plain {
		registration.formatter.Format(p.pkg, inLevel, depth+1, entries...)
	} else {
		registration.formatter.FormatKV(p.pkg, inLevel, depth+1, entries...)
	}
	if inLevel == CRITICAL {
		_ = FlushFormatterWithin(registration.formatter, CriticalFlushTimeout)
	}
}

func (p *PackageLogger) internalLogf(depth int, inLevel LogLevel, format string, args ...any) {
	registration := p.prepare(inLevel)
	if registration == nil {
		return
	}
	defer registration.active.Done()
	defer releaseOutput(acquireOutput(registration.formatter))
	message := fmt.Sprintf(format, args...)
	if len(p.values) > 0 {
		entries := append(slices.Clone(p.values), "msg", message)
		registration.formatter.FormatKV(p.pkg, inLevel, depth+1, entries...)
	} else {
		registration.formatter.Format(p.pkg, inLevel, depth+1, message)
	}
	if inLevel == CRITICAL {
		_ = FlushFormatterWithin(registration.formatter, CriticalFlushTimeout)
	}
}

// LevelAt reports whether the logger's threshold includes l.
func (p *PackageLogger) LevelAt(l LogLevel) bool {
	return p.currentLevel() >= l
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

// Flush flushes the current global formatter, or does nothing when it is nil.
// It does not guarantee durable storage. See FlushError for error reporting.
func (p *PackageLogger) Flush() {
	_ = p.FlushError()
}

// FlushError flushes the current formatter and supported downstream buffers,
// including queued writes, and reports errors when supported by the formatter.
// It returns nil when output is disabled. It does not guarantee durable storage.
func (p *PackageLogger) FlushError() error {
	logger.Lock()
	registration := logger.current
	if registration == nil || registration.formatter == nil {
		logger.Unlock()
		return nil
	}
	registration.active.Add(1)
	logger.Unlock()
	defer registration.active.Done()
	defer releaseOutput(acquireOutput(registration.formatter))
	return FlushFormatter(registration.formatter)
}
