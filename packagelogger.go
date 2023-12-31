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

// ExitFunc can be overriten
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

// WithValues adds some key-value pairs of context to a logger.
// See Info for documentation on how key/value pairs work.
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
			entries = append(flatten(false, p.values...), entries)
		}

		logger.formatter.Format(p.pkg, inLevel, depth+1, entries...)
	}
}

// LevelAt returns the current log level
func (p *PackageLogger) LevelAt(l LogLevel) bool {
	logger.Lock()
	defer logger.Unlock()
	return p.level >= l
}

// Logf a formatted string at any level between ERROR and TRACE
func (p *PackageLogger) Logf(l LogLevel, format string, args ...any) {
	p.internalLogf(calldepth, l, format, args...)
}

// Log a message at any level between ERROR and TRACE
func (p *PackageLogger) Log(l LogLevel, args ...any) {
	p.internalLog(plain, calldepth, l, args...)
}

// Panic and fatal

// Panicf is implementation for stdlib compatibility
func (p *PackageLogger) Panicf(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	p.internalLog(plain, calldepth, CRITICAL, s)
	panic(s)
}

// Panic is implementation for stdlib compatibility
func (p *PackageLogger) Panic(args ...any) {
	s := fmt.Sprint(args...)
	p.internalLog(plain, calldepth, CRITICAL, s)
	panic(s)
}

// Fatalf is implementation for stdlib compatibility
func (p *PackageLogger) Fatalf(format string, args ...any) {
	p.internalLogf(calldepth, CRITICAL, format, args...)
	ExitFunc(1)
}

// Fatal is implementation for stdlib compatibility
func (p *PackageLogger) Fatal(args ...any) {
	s := fmt.Sprint(args...)
	p.internalLog(plain, calldepth, CRITICAL, s)
	ExitFunc(1)
}

// Error Functions

// Errorf is implementation for stdlib compatibility
func (p *PackageLogger) Errorf(format string, args ...any) {
	p.internalLogf(calldepth, ERROR, format, args...)
}

// Error is implementation for stdlib compatibility
func (p *PackageLogger) Error(entries ...any) {
	p.internalLog(plain, calldepth, ERROR, entries...)
}

// Warning Functions

// Warningf is implementation for stdlib compatibility
func (p *PackageLogger) Warningf(format string, args ...any) {
	p.internalLogf(calldepth, WARNING, format, args...)
}

// Warning is implementation for stdlib compatibility
func (p *PackageLogger) Warning(entries ...any) {
	p.internalLog(plain, calldepth, WARNING, entries...)
}

// Notice Functions

// Noticef is implementation for stdlib compatibility
func (p *PackageLogger) Noticef(format string, args ...any) {
	p.internalLogf(calldepth, NOTICE, format, args...)
}

// Notice is implementation for stdlib compatibility
func (p *PackageLogger) Notice(entries ...any) {
	p.internalLog(plain, calldepth, NOTICE, entries...)
}

// Info Functions

// Infof is implementation for stdlib compatibility
func (p *PackageLogger) Infof(format string, args ...any) {
	p.internalLogf(calldepth, INFO, format, args...)
}

// Info is implementation for stdlib compatibility
func (p *PackageLogger) Info(entries ...any) {
	p.internalLog(plain, calldepth, INFO, entries...)
}

// KV prints key=value pairs
func (p *PackageLogger) KV(l LogLevel, entries ...any) {
	p.internalLog(kv, calldepth, l, entries...)
}

// ContextKV logs entries in "key1=value1, ..., keyN=valueN" format,
// and add log entries from ctx as well.
// ContextWithKV method can be used to add extra values to context
func (p *PackageLogger) ContextKV(ctx context.Context, l LogLevel, entries ...any) {
	extra := ContextEntries(ctx)
	if len(extra) > 0 {
		entries = append(extra, entries...)
	}
	p.internalLog(kv, calldepth, l, entries...)
}

// Debug Functions

// Debugf is implementation for stdlib compatibility
func (p *PackageLogger) Debugf(format string, args ...any) {
	p.internalLogf(calldepth, DEBUG, format, args...)
}

// Debug is implementation for stdlib compatibility
func (p *PackageLogger) Debug(entries ...any) {
	p.internalLog(plain, calldepth, DEBUG, entries...)
}

// Trace Functions

// Tracef is implementation for stdlib compatibility
func (p *PackageLogger) Tracef(format string, args ...any) {
	p.internalLogf(calldepth, TRACE, format, args...)
}

// Trace is implementation for stdlib compatibility
func (p *PackageLogger) Trace(entries ...any) {
	p.internalLog(plain, calldepth, TRACE, entries...)
}

// Flush the logs
func (p *PackageLogger) Flush() {
	logger.Lock()
	defer logger.Unlock()
	logger.formatter.Flush()
}
