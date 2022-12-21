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

// FormatterOption specifies additional formatter options
type FormatterOption int

const (
	// FormatWithCaller allows to configure if the caller shall be logged
	FormatWithCaller FormatterOption = iota + 1
	// FormatNoCaller disables log the caller
	FormatNoCaller
	// FormatSkipTime allows to configure skipping the time log
	FormatSkipTime
	// FormatSkipLevel allows to configure skipping the level log
	FormatSkipLevel
	// FormatWithLocation allows to print the file:line for each log
	FormatWithLocation
	// FormatWithColor allows to print color logs
	FormatWithColor
	// FormatPrintEmpty allows to print empty values
	FormatPrintEmpty
)

// Formatter defines an interface for formatting logs
type Formatter interface {
	// Format log entry string to the stream,
	// the entries are separated by space
	Format(pkg string, level LogLevel, depth int, entries ...interface{})
	// FormatKV log entry string to the stream,
	// the entries are key/value pairs
	FormatKV(pkg string, level LogLevel, depth int, entries ...interface{})
	// Flush the logs
	Flush()
	// Options allows to configure formatter behavior
	Options(ops ...FormatterOption) Formatter
}

// TimeNowFn to override in unit tests
var TimeNowFn = time.Now

// NewStringFormatter returns string-based formatter
func NewStringFormatter(w io.Writer) Formatter {
	return &StringFormatter{
		w: bufio.NewWriter(w),
		config: config{
			withCaller: true,
			skipTime:   false,
		},
	}
}

// StringFormatter defines string-based formatter
type StringFormatter struct {
	config
	w *bufio.Writer
}

// Options allows to configure formatter behavior
func (s *StringFormatter) Options(ops ...FormatterOption) Formatter {
	s.config.options(ops)
	return s
}

// FormatKV log entry string to the stream,
// the entries are key/value pairs
func (s *StringFormatter) FormatKV(pkg string, l LogLevel, depth int, entries ...interface{}) {
	s.format(pkg, l, depth+1, false, flatten(s.config.printEmpty, entries...)...)
}

// Format log entry string to the stream
func (s *StringFormatter) Format(pkg string, l LogLevel, depth int, entries ...interface{}) {
	s.format(pkg, l, depth+1, true, entries...)
}

func (s *StringFormatter) format(pkg string, l LogLevel, depth int, escape bool, entries ...interface{}) {
	if !s.skipTime {
		now := TimeNowFn().UTC()
		s.w.WriteString("time=")
		s.w.WriteString(now.Format(time.RFC3339))
		s.w.WriteByte(' ')
	}
	if !s.skipLevel {
		s.w.WriteString("level=")
		s.w.WriteString(l.Char())
		s.w.WriteByte(' ')
	}

	params := writeEntriesParams{
		pkg:          pkg,
		separator:    " ",
		depth:        depth + 1,
		withCaller:   s.withCaller,
		withLocation: s.withLocation,
		escape:       escape,
		printEmpty:   s.printEmpty,
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

func writeEntries(w *bufio.Writer, p *writeEntriesParams, entries ...interface{}) {
	if p.pkg != "" {
		w.WriteString("pkg=")
		w.WriteString(p.pkg)
		w.WriteString(p.separator)
	}

	if p.withLocation || p.withCaller {
		caller, file, line := Caller(p.depth + 1)

		if p.withLocation {
			w.WriteString("src=")
			// It's always the same number of frames to the user's call.
			w.WriteString(fmt.Sprintf("%s:%d", file, line))
			w.WriteString(p.separator)
		}

		if p.withCaller {
			w.WriteString("func=")
			w.WriteString(caller)
			w.WriteString(p.separator)
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
			w.WriteString(str)
			if i+1 < count {
				w.WriteString(p.separator)
			}
		}
	}

	if p.colorOff {
		w.Write(ColorOff)
	}

	l := len(str)
	endsInNL := l > 0 && str[l-1] == '\n'
	if !endsInNL {
		w.WriteByte('\n')
	}
}

// Flush the logs
func (s *StringFormatter) Flush() {
	s.w.Flush()
}

// NewPrettyFormatter returns an instance of PrettyFormatter
func NewPrettyFormatter(w io.Writer) Formatter {
	return &PrettyFormatter{
		w: bufio.NewWriter(w),
		config: config{
			withCaller:   true,
			skipTime:     false,
			withLocation: false,
			color:        false,
		},
	}
}

// PrettyFormatter provides default logs format
type PrettyFormatter struct {
	config
	w *bufio.Writer
}

// Options allows to configure formatter behavior
func (c *PrettyFormatter) Options(ops ...FormatterOption) Formatter {
	c.config.options(ops)
	return c
}

// FormatKV log entry string to the stream,
// the entries are key/value pairs
func (c *PrettyFormatter) FormatKV(pkg string, l LogLevel, depth int, entries ...interface{}) {
	c.format(pkg, l, depth+1, false, flatten(c.printEmpty, entries...)...)
}

// Format log entry string to the stream
func (c *PrettyFormatter) Format(pkg string, l LogLevel, depth int, entries ...interface{}) {
	c.format(pkg, l, depth+1, true, entries...)
}

// Format log entry string to the stream
func (c *PrettyFormatter) format(pkg string, l LogLevel, depth int, escape bool, entries ...interface{}) {
	if !c.skipTime {
		now := TimeNowFn()
		ts := now.Format("2006-01-02 15:04:05")
		c.w.WriteString(ts)
		ms := now.Nanosecond() / 1000
		c.w.WriteString(fmt.Sprintf(".%06d ", ms))
	}
	if c.color {
		c.w.Write(LevelColors[l])
	}
	if !c.skipLevel {
		c.w.WriteString(l.Char())
		c.w.WriteString(" | ")
	}
	params := writeEntriesParams{
		pkg:          pkg,
		separator:    ", ",
		depth:        depth + 1,
		withCaller:   c.withCaller,
		withLocation: c.withLocation,
		escape:       escape,
		colorOff:     c.color,
		printEmpty:   c.printEmpty,
	}

	writeEntries(c.w, &params, entries...)

	c.Flush()
}

// Flush the logs
func (c *PrettyFormatter) Flush() {
	c.w.Flush()
}

// color pallete map
var (
	ColorOff = []byte("\033[0m")
	// colorRed         = []byte("\033[0;31m")
	// colorGreen       = []byte("\033[0;32m")
	// colorOrange      = []byte("\033[0;33m")
	// colorBlue        = []byte("\033[0;34m")
	// colorPurple      = []byte("\033[0;35m")
	// colorCyan        = []byte("\033[0;36m")
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
func (*NilFormatter) FormatKV(pkg string, level LogLevel, depth int, entries ...interface{}) {
}

// Format does nothing.
func (*NilFormatter) Format(_ string, _ LogLevel, _ int, _ ...interface{}) {
	// noop
}

// Flush is included so that the interface is complete, but is a no-op.
func (*NilFormatter) Flush() {
	// noop
}

func flatten(printEmpty bool, kvList ...interface{}) []interface{} {
	size := len(kvList)
	list := make([]interface{}, 0, size/2)

	for i, j := 0, 0; i < size; i += 2 {
		k, ok := kvList[i].(string)
		if !ok {
			panic(fmt.Sprintf("key is not a string: %v", EscapedString(kvList[i])))
		}
		var v interface{}
		if i+1 < size {
			v = kvList[i+1]
		}
		if v == nil && !printEmpty {
			continue
		}
		val := EscapedString(v)
		if val != `""` || printEmpty {
			list = append(list, k+"="+val)
			j++
		}
	}
	return list
}

// EscapedString returns string value stuitable for logging
func EscapedString(value interface{}) string {
	switch typ := value.(type) {
	case error:
		value = fmt.Sprintf("%+v", typ)
	case string:
		value = strings.TrimSpace(typ)
		// pass through for encoding
	case uint64:
		return strconv.FormatUint(typ, 10)
	case uint:
		return strconv.FormatUint(uint64(typ), 10)
	case int64:
		return strconv.FormatInt(typ, 10)
	case int:
		return strconv.FormatInt(int64(typ), 10)
	case bool:
		if typ {
			return "true"
		}
		return "false"
	case []byte:
		return "\"" + base64.StdEncoding.EncodeToString(typ) + "\""
	case reflect.Type:
		value = typ.String()
	case time.Time:
		value = typ.Format(time.RFC3339)
		// pass through for encoding
	case fmt.Stringer:
		value = strings.TrimSpace(typ.String())
		// pass through for encoding
	default:
		// keep as is to json.Encode
	}

	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	encoder.Encode(value)
	return strings.TrimSpace(buffer.String())
}

// Caller returns caller function name, and location
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

		// remove package name
		idx := strings.Index(name, ".")
		if idx >= 0 {
			name = name[idx+1:]
			if name[0] == '(' {
				idx = strings.Index(name, ".")
				if idx >= 0 {
					name = name[idx+1:]
				}
			}
		}
		return name, file, line
	}
	return "func", file, line
}

type config struct {
	withCaller   bool
	skipTime     bool
	skipLevel    bool
	printEmpty   bool
	withLocation bool
	color        bool
}

// Options allows to configure formatter behavior
func (c *config) options(ops []FormatterOption) {
	for _, op := range ops {
		switch op {
		case FormatWithCaller:
			c.withCaller = true
		case FormatNoCaller:
			c.withCaller = false
		case FormatSkipTime:
			c.skipTime = true
		case FormatSkipLevel:
			c.skipLevel = true
		case FormatWithLocation:
			c.withLocation = true
		case FormatWithColor:
			c.color = true
		case FormatPrintEmpty:
			c.printEmpty = true
		}
	}
}
