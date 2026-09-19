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
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Formatter formats records and writes them to a destination. PackageLogger
// serializes output separately from configuration; direct calls require synchronization.
// See ErrorFormatter for optional error reporting and downstream flushing.
// Formatters, writers, and value serializers must not recursively emit logs
// through the same output path; configuration access is safe.
type Formatter interface {
	// Format emits plain entries using the formatter's message representation.
	Format(pkg string, level LogLevel, depth int, entries ...any)
	// FormatKV emits alternating string keys and values as structured fields.
	FormatKV(pkg string, level LogLevel, depth int, entries ...any)
	// Flush flushes the formatter's buffer, not necessarily downstream buffers
	// or durable storage. Implementations cannot return errors through this API.
	Flush()
	// Options mutates formatter configuration. Apply options before publishing
	// the formatter with SetFormatter, or while logging is stopped.
	Options(ops ...FormatterOption) Formatter
}

// TimeNowFn returns the current time. Override and restore it only while logging
// is stopped, for example in tests that do not run in parallel.
var TimeNowFn = time.Now

// NewDefaultFormatter returns the default text formatter, a PrettyFormatter.
func NewDefaultFormatter(out io.Writer, ops ...FormatterOption) Formatter {
	return NewPrettyFormatter(out, ops...)
}

// NewStringFormatter returns a text formatter writing to w. Options apply
// before the destination is bound, so WithSink takes effect immediately.
func NewStringFormatter(w io.Writer, ops ...FormatterOption) Formatter {
	s := &StringFormatter{}
	s.WithCaller = true
	s.MaxLogLength = DefaultMaxLogMessageLength
	s.Apply(ops...)
	s.Bind(w, s.sink)
	return s
}

// StringFormatter renders text records. Do not copy it after first use.
type StringFormatter struct {
	Config
	Output
}

// Options allows to configure formatter behavior
func (s *StringFormatter) Options(ops ...FormatterOption) Formatter {
	s.Apply(ops...)
	s.Rebind(s.sink)
	return s
}

// FormatKV log entry string to the stream,
// the entries are key/value pairs
func (s *StringFormatter) FormatKV(pkg string, l LogLevel, depth int, entries ...any) {
	s.format(pkg, l, depth+1, false, s.flatten(entries...)...)
}

// Format log entry string to the stream
func (s *StringFormatter) Format(pkg string, l LogLevel, depth int, entries ...any) {
	s.format(pkg, l, depth+1, true, entries...)
}

func (s *StringFormatter) format(pkg string, l LogLevel, depth int, escape bool, entries ...any) {
	record := s.Buffer()
	w := record.Buffer()
	if !s.SkipTime {
		now := TimeNowFn().UTC()
		_, _ = w.WriteString("time=")
		_, _ = w.WriteString(now.Format(time.RFC3339))
		_, _ = w.WriteString(" ")
	}
	if !s.SkipLevel {
		_, _ = w.WriteString("level=")
		_, _ = w.WriteString(l.Char())
		_ = w.WriteByte(' ')
	}

	params := writeEntriesParams{
		pkg:          pkg,
		separator:    " ",
		depth:        depth + 1,
		withCaller:   s.WithCaller,
		withLocation: s.WithLocation,
		escape:       escape,
		printEmpty:   s.PrintEmpty,
	}
	writeEntries(w, &params, entries...)
	s.Emit(record, l)
}

type writeEntriesParams struct {
	pkg          string
	separator    string
	depth        int
	withCaller   bool
	withLocation bool
	escape       bool
	colorOff     bool
	printEmpty   bool
}

func writeEntries(w *bytes.Buffer, p *writeEntriesParams, entries ...any) {
	if p.pkg != "" {
		_, _ = w.WriteString("pkg=")
		_, _ = w.WriteString(p.pkg)
		_, _ = w.WriteString(p.separator)
	}

	if p.withLocation || p.withCaller {
		caller, file, line := Caller(p.depth + 1)

		if p.withLocation {
			_, _ = w.WriteString("src=")
			// It's always the same number of frames to the user's call.
			_, _ = fmt.Fprintf(w, "%s:%d", file, line)
			_, _ = w.WriteString(p.separator)
		}

		if p.withCaller {
			_, _ = w.WriteString("func=")
			_, _ = w.WriteString(caller)
			_, _ = w.WriteString(p.separator)
		}
	}

	var str string
	for i, count := 0, len(entries); i < count; i++ {
		if p.escape {
			str = EscapedString(entries[i])
		} else {
			str = fmt.Sprint(entries[i])
		}
		if str != "" || p.printEmpty {
			_, _ = w.WriteString(str)
			if i+1 < count {
				_, _ = w.WriteString(p.separator)
			}
		}
	}

	if p.colorOff {
		_, _ = w.Write(ColorOff)
	}

	l := len(str)
	endsInNL := l > 0 && str[l-1] == '\n'
	if !endsInNL {
		_ = w.WriteByte('\n')
	}
}

// NewPrettyFormatter returns a readable text formatter writing to w. Options
// apply before the destination is bound, so WithSink takes effect immediately.
func NewPrettyFormatter(w io.Writer, ops ...FormatterOption) Formatter {
	c := &PrettyFormatter{}
	c.WithCaller = true
	c.MaxLogLength = DefaultMaxLogMessageLength
	c.Apply(ops...)
	c.Bind(w, c.sink)
	return c
}

// PrettyFormatter renders readable text with optional colors. Do not copy it
// after first use.
type PrettyFormatter struct {
	Config
	Output
}

// Options allows to configure formatter behavior
func (c *PrettyFormatter) Options(ops ...FormatterOption) Formatter {
	c.Apply(ops...)
	c.Rebind(c.sink)
	return c
}

// FormatKV log entry string to the stream,
// the entries are key/value pairs
func (c *PrettyFormatter) FormatKV(pkg string, l LogLevel, depth int, entries ...any) {
	c.format(pkg, l, depth+1, false, c.flatten(entries...)...)
}

// Format log entry string to the stream
func (c *PrettyFormatter) Format(pkg string, l LogLevel, depth int, entries ...any) {
	c.format(pkg, l, depth+1, true, entries...)
}

// Format log entry string to the stream
func (c *PrettyFormatter) format(pkg string, l LogLevel, depth int, escape bool, entries ...any) {
	record := c.Buffer()
	w := record.Buffer()
	if !c.SkipTime {
		now := TimeNowFn()
		ts := now.Format("2006-01-02 15:04:05")
		_, _ = w.WriteString(ts)
		ms := now.Nanosecond() / 1000
		_, _ = fmt.Fprintf(w, ".%06d ", ms)
	}
	if c.WithColor {
		_, _ = w.Write(LevelColors[l])
	}
	if !c.SkipLevel {
		_, _ = w.WriteString(l.Char())
		_, _ = w.WriteString(" | ")
	}
	params := writeEntriesParams{
		pkg:          pkg,
		separator:    ", ",
		depth:        depth + 1,
		withCaller:   c.WithCaller,
		withLocation: c.WithLocation,
		escape:       escape,
		colorOff:     c.WithColor,
		printEmpty:   c.PrintEmpty,
	}

	writeEntries(w, &params, entries...)

	c.Emit(record, l)
}

// ColorOff resets ANSI color to terminal default.
var ColorOff = []byte("\033[0m")

var (
	colorLightRed    = []byte("\033[0;91m") // ERROR
	colorLightGreen  = []byte("\033[0;92m") // NOTICE
	colorLightOrange = []byte("\033[0;93m") // WARN
	colorLightCyan   = []byte("\033[0;96m") // INFO
	colorGray        = []byte("\033[0;37m") // TRACE
	colorDebug       = []byte("\033[0;35m") // DEBUG
)

// LevelColors provides colors map
var LevelColors = map[LogLevel][]byte{
	CRITICAL: colorLightRed,
	ERROR:    colorLightRed,
	WARNING:  colorLightOrange,
	NOTICE:   colorLightGreen,
	INFO:     colorLightCyan,
	DEBUG:    colorDebug,
	TRACE:    colorGray,
}

// NilFormatter is a no-op log formatter that does nothing.
type NilFormatter struct {
}

// NewNilFormatter is a helper to produce a new LogFormatter struct. It logs no
// messages so that you can cause part of your logging to be silent.
func NewNilFormatter() Formatter {
	return &NilFormatter{}
}

// Options allows to configure formatter behavior
func (c *NilFormatter) Options(ops ...FormatterOption) Formatter {
	return c
}

// FormatKV log entry string to the stream,
// the entries are key/value pairs
func (*NilFormatter) FormatKV(pkg string, level LogLevel, depth int, entries ...any) {
}

// Format does nothing.
func (*NilFormatter) Format(_ string, _ LogLevel, _ int, _ ...any) {
	// noop
}

// Flush is included so that the interface is complete, but is a no-op.
func (*NilFormatter) Flush() {
	// noop
}

// Concurrent reports that discarding records needs no output serialization.
func (*NilFormatter) Concurrent() bool {
	return true
}

func (c *Config) flatten(kvList ...any) []any {
	size := len(kvList)
	list := make([]any, 0, size/2)

	for i, j := 0, 0; i < size; i += 2 {
		k, ok := kvList[i].(string)
		if !ok {
			panic(fmt.Sprintf("key is not a string: %v", EscapedString(kvList[i])))
		}
		var v any
		if i+1 < size {
			v = kvList[i+1]
		}
		if v == nil && !c.PrintEmpty {
			continue
		}
		val := EscapedString(v)
		if c.PrintEmpty || (val != `""` && val != "") {
			if c.MaxLogLength > 0 && len(val) > c.MaxLogLength {
				if val[0] == '"' {
					val = val[:c.MaxLogLength] + "...\""
				} else {
					val = val[:c.MaxLogLength] + "..."
				}
			}
			list = append(list, k+"="+val)
			j++
		}
	}
	return list
}

// WithValueString supplies a display name for values such as generated enums.
type WithValueString interface {
	// ValueString returns the value's human-readable name.
	ValueString() string
}

// EscapedInt64 formats an integer for text logs, prefixing an underscore when
// its magnitude is at least 9007199254740991. The result is not always JSON.
func EscapedInt64(value int64) string {
	if value <= -9007199254740991 || value >= 9007199254740991 {
		return "_" + strconv.FormatInt(value, 10)
	}
	return strconv.FormatInt(value, 10)
}

const (
	min64NumberLen = 16 // len(9007199254740991)
	max64NumberLen = 19 // len(9007199254740991)
)

// EscapedUInt64 formats an integer for text logs, prefixing an underscore when
// it is at least 9007199254740991. The result is not always JSON.
func EscapedUInt64(value uint64) string {
	str := strconv.FormatUint(value, 10)
	// JavaScript max number (9007199254740991) exceeding 15 digits
	if value >= 9007199254740991 {
		str = "_" + str
	}
	return str
}

// jsonEscaper bundles a reusable buffer with its JSON encoder so both can be
// pooled together, avoiding an allocation per call for each.
type jsonEscaper struct {
	buf bytes.Buffer
	enc *json.Encoder
}

// maxPooledBufferSize caps the size of buffers we are willing to keep in the
// pool. Encoding an unusually large value should not pin a large buffer in
// memory for the lifetime of the process.
const maxPooledBufferSize = 64 << 10 // 64 KiB

var escaperPool = sync.Pool{
	New: func() any {
		e := &jsonEscaper{}
		e.enc = json.NewEncoder(&e.buf)
		e.enc.SetEscapeHTML(false)
		return e
	},
}

// jsonEncode encodes value as JSON (without HTML escaping) using a pooled
// buffer/encoder. It is safe for concurrent use: each call obtains its own
// escaper from the pool, and the returned string is a copy independent of the
// pooled buffer, so the buffer can be safely reset and reused afterwards.
func jsonEncode(value any) string {
	e := escaperPool.Get().(*jsonEscaper)
	e.buf.Reset()
	// json.Encoder.Encode appends a trailing newline, hence the TrimSpace.
	_ = e.enc.Encode(value)
	out := strings.TrimSpace(e.buf.String())

	// Avoid retaining oversized buffers in the pool.
	if e.buf.Cap() <= maxPooledBufferSize {
		escaperPool.Put(e)
	}
	return out
}

func escapeString(value string) string {
	s := strings.TrimSpace(value)
	size := len(s)
	if size > 2 && s[0] == '"' && s[size-1] == '"' {
		s = s[1 : size-1]
		size -= 2
	}
	if size >= min64NumberLen && size <= max64NumberLen && isNumber(s) && s >= "9007199254740991" {
		return "_" + s
	}
	if needsEncoding(s) {
		return jsonEncode(s)
	}
	return s
}

// EscapedString renders a value for text logs, quoting selected strings and
// encoding composite values as JSON. It is not a JSON serializer: simple
// strings, times, durations, and underscore-prefixed large integers may be bare.
// Unsupported JSON values produce an empty string; encoding errors are ignored.
func EscapedString(value any) string {
	switch typ := value.(type) {
	case error:
		return escapeString(fmt.Sprintf("%+v", typ))
	case time.Duration:
		return typ.String()
	case json.RawMessage:
		return string(typ)
	case string:
		return escapeString(typ)
	case uint64:
		return EscapedUInt64(typ)
	case uint32:
		return strconv.FormatUint(uint64(typ), 10)
	case uint:
		return EscapedUInt64(uint64(typ))
	case int64:
		return EscapedInt64(typ)
	case int32:
		return strconv.FormatInt(int64(typ), 10)
	case int:
		return EscapedInt64(int64(typ))
	case bool:
		if typ {
			return "true"
		}
		return "false"
	case []byte:
		return "\"" + base64.StdEncoding.EncodeToString(typ) + "\""
	case reflect.Type:
		return escapeString(typ.String())
	case time.Time:
		return typ.UTC().Format(time.RFC3339)
	case *time.Time:
		if typ == nil {
			return "null"
		}
		return typ.UTC().Format(time.RFC3339)
	case fmt.Stringer:
		return escapeString(typ.String())
	default:
		// Handle proto enums
		if en, ok := value.(WithValueString); ok {
			return fmt.Sprintf(`"%s (%v)"`, en.ValueString(), value)
		}
		// pass through for encoding
	}

	return jsonEncode(value)
}

// callerInfo is one resolved call site.
type callerInfo struct {
	name string
	file string
	line int
}

// callerCache memoizes program counter resolution. Unwinding the stack and
// trimming the function and file names dominates the cost of a small record,
// while the number of distinct logging call sites is bounded by the program.
var callerCache sync.Map // uintptr -> callerInfo

// Caller returns the function name, file, and line number of the caller at the given depth.
func Caller(depth int) (name string, file string, line int) {
	var pcs [1]uintptr
	// runtime.Callers counts itself, so depth+1 selects runtime.Caller's frame.
	if runtime.Callers(depth+1, pcs[:]) < 1 {
		return "func", "???", 1
	}
	if cached, ok := callerCache.Load(pcs[0]); ok {
		info := cached.(callerInfo)
		return info.name, info.file, info.line
	}
	info := resolveCaller(pcs[0])
	callerCache.Store(pcs[0], info)
	return info.name, info.file, info.line
}

// resolveCaller renders one program counter the way Caller reports it.
func resolveCaller(pc uintptr) callerInfo {
	frame, _ := runtime.CallersFrames([]uintptr{pc}).Next()
	info := callerInfo{
		name: "func",
		file: frame.File,
		line: frame.Line,
	}
	if info.file == "" {
		info.file = "???"
	} else if slash := strings.LastIndex(info.file, "/"); slash >= 0 {
		info.file = info.file[slash+1:]
	}
	if info.line < 0 {
		info.line = 0 // not a real line number
	}
	if frame.Function == "" {
		return info
	}
	name := path.Base(frame.Function)
	name = removePart(name, "[", "]")
	name = removePart(name, "(", ")")

	// remove package name
	if idx := strings.Index(name, "."); idx >= 0 {
		name = strings.TrimLeft(name[idx+1:], ".")
	}
	info.name = name
	return info
}

func removePart(val, open, close string) string {
	b, a, ok := strings.Cut(val, open)
	if !ok {
		return val
	}
	_, c, ok := strings.Cut(a, close)
	if !ok {
		return b
	}
	return b + c
}

func isNumber(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func needsEncoding(s string) bool {
	for i, c := range s {
		if i > 32 || c == ' ' || c == '\n' || c == '\t' || c == '[' || c == '{' || c == '(' || c == '`' || c == '"' || c == '\'' || c == '*' || c == '\\' || c == ':' || c == '/' {
			return true
		}
	}
	return false
}
