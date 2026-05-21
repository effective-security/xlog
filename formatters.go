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
	"bufio"
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
	"time"
)

// Formatter defines an interface for formatting logs
type Formatter interface {
	// Format log entry string to the stream,
	// the entries are separated by space
	Format(pkg string, level LogLevel, depth int, entries ...any)
	// FormatKV log entry string to the stream,
	// the entries are key/value pairs
	FormatKV(pkg string, level LogLevel, depth int, entries ...any)
	// Flush the logs
	Flush()
	// Options allows to configure formatter behavior
	Options(ops ...FormatterOption) Formatter
}

// TimeNowFn returns the current time; it may be overridden in tests for deterministic behavior.
var TimeNowFn = time.Now

// NewStringFormatter returns string-based formatter
func NewStringFormatter(w io.Writer) Formatter {
	return &StringFormatter{
		w: bufio.NewWriter(w),
		Config: Config{
			WithCaller: true,
			SkipTime:   false,
		},
	}
}

// StringFormatter defines string-based formatter
type StringFormatter struct {
	Config
	w *bufio.Writer
}

// Options allows to configure formatter behavior
func (s *StringFormatter) Options(ops ...FormatterOption) Formatter {
	s.Apply(ops...)
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
	if !s.SkipTime {
		now := TimeNowFn().UTC()
		_, _ = s.w.WriteString("time=")
		_, _ = s.w.WriteString(now.Format(time.RFC3339))
		_, _ = s.w.WriteString(" ")
	}
	if !s.SkipLevel {
		_, _ = s.w.WriteString("level=")
		_, _ = s.w.WriteString(l.Char())
		_ = s.w.WriteByte(' ')
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
	writeEntries(s.w, &params, entries...)
	s.Flush()
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

func writeEntries(w *bufio.Writer, p *writeEntriesParams, entries ...any) {
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

// Flush the logs
func (s *StringFormatter) Flush() {
	_ = s.w.Flush()
}

// NewPrettyFormatter returns an instance of PrettyFormatter
func NewPrettyFormatter(w io.Writer) Formatter {
	return &PrettyFormatter{
		w: bufio.NewWriter(w),
		Config: Config{
			WithCaller:   true,
			SkipTime:     false,
			WithLocation: false,
			WithColor:    false,
		},
	}
}

// PrettyFormatter provides default logs format
type PrettyFormatter struct {
	Config
	w *bufio.Writer
}

// Options allows to configure formatter behavior
func (c *PrettyFormatter) Options(ops ...FormatterOption) Formatter {
	c.Apply(ops...)
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
	if !c.SkipTime {
		now := TimeNowFn()
		ts := now.Format("2006-01-02 15:04:05")
		_, _ = c.w.WriteString(ts)
		ms := now.Nanosecond() / 1000
		_, _ = fmt.Fprintf(c.w, ".%06d ", ms)
	}
	if c.WithColor {
		_, _ = c.w.Write(LevelColors[l])
	}
	if !c.SkipLevel {
		_, _ = c.w.WriteString(l.Char())
		_, _ = c.w.WriteString(" | ")
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

	writeEntries(c.w, &params, entries...)

	c.Flush()
}

// Flush the logs
func (c *PrettyFormatter) Flush() {
	_ = c.w.Flush()
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
		if val != `""` || c.PrintEmpty {
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

type WithValueString interface {
	ValueString() string
}

// EscapedInt64 returns a string suitable for logging.
func EscapedInt64(value int64) string {
	if value <= -9007199254740991 || value >= 9007199254740991 {
		return "\"_" + strconv.FormatInt(value, 10) + "\""
	}
	return strconv.FormatInt(value, 10)
}

// EscapedUInt64 returns a string suitable for logging.
func EscapedUInt64(value uint64) string {
	// JavaScript max number (9007199254740991) exceeding 15 digits
	if value >= 9007199254740991 {
		return "\"_" + strconv.FormatUint(value, 10) + "\""
	}
	return strconv.FormatUint(value, 10)
}

// EscapedString returns a JSON-escaped string representation of the value, suitable for logging.
func EscapedString(value any) string {
	switch typ := value.(type) {
	case error:
		value = fmt.Sprintf("%+v", typ)
		// pass through for encoding
	case time.Duration:
		return typ.String()
	case json.RawMessage:
		return string(typ)
	case string:
		value = strings.TrimSpace(typ)
		// pass through for encoding
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
		value = typ.String()
		// pass through for encoding
	case time.Time:
		return typ.UTC().Format(time.RFC3339)
	case *time.Time:
		if typ == nil {
			return "null"
		}
		return typ.UTC().Format(time.RFC3339)
	case fmt.Stringer:
		value = strings.TrimSpace(typ.String())
		// pass through for encoding
	default:
		// Handle proto enums
		if en, ok := value.(WithValueString); ok {
			return fmt.Sprintf(`"%s (%v)"`, en.ValueString(), value)
		}
		// pass through for encoding
	}

	// Create a new buffer for each call to avoid concurrency issues
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	return strings.TrimSpace(buffer.String())
}

// Caller returns the function name, file, and line number of the caller at the given depth.
func Caller(depth int) (name string, file string, line int) {
	pc, file, line, ok := runtime.Caller(depth)

	if !ok {
		file = "???"
		line = 1
	} else {
		slash := strings.LastIndex(file, "/")
		if slash >= 0 {
			file = file[slash+1:]
		}
	}
	if line < 0 {
		line = 0 // not a real line number
	}

	details := runtime.FuncForPC(pc)
	if ok && details != nil {
		name := path.Base(details.Name())
		name = removePart(name, "[", "]")
		name = removePart(name, "(", ")")

		// remove package name
		idx := strings.Index(name, ".")
		if idx >= 0 {
			name = strings.TrimLeft(name[idx+1:], ".")
		}
		return name, file, line
	}
	return "func", file, line
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
