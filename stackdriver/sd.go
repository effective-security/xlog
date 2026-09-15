package stackdriver

// Copyright 2022, Denis Issoupov
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

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"runtime"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/xlog"
)

type severity string

const (
	severityDebug    severity = "DEBUG"
	severityInfo     severity = "INFO"
	severityNotice   severity = "NOTICE"
	severityWarning  severity = "WARNING"
	severityError    severity = "ERROR"
	severityCritical severity = "CRITICAL"
	//severityAlert    severity = "ALERT"
)

var levelsToSeverity = map[xlog.LogLevel]severity{
	xlog.DEBUG:    severityDebug,
	xlog.TRACE:    severityDebug,
	xlog.INFO:     severityInfo,
	xlog.NOTICE:   severityNotice,
	xlog.WARNING:  severityWarning,
	xlog.ERROR:    severityError,
	xlog.CRITICAL: severityCritical,
}

// MaxLogMessageLength limits plain messages in bytes before appending "...".
// It does not limit KV fields. Configure it before logging; negative values panic
// when formatting a message. This is independent of xlog.FormatMaxLogLength.
var MaxLogMessageLength = 2 * 1024

// formatter provides logs format for StackDriver
type formatter struct {
	xlog.Config
	xlog.Output
	logName string
}

// NewFormatter returns a Stackdriver formatter for xlog, writing log entries
// as Stackdriver-compatible JSON. logName sets the Stackdriver log name.
// Options apply before the destination is bound, so xlog.WithSink takes effect
// immediately and delivers rendered records on the sink worker.
func NewFormatter(w io.Writer, logName string, ops ...xlog.FormatterOption) xlog.Formatter {
	c := &formatter{
		logName: logName,
	}
	c.WithCaller = true
	c.MaxLogLength = xlog.DefaultMaxLogMessageLength
	c.Apply(ops...)
	c.Bind(w, c.Sink())
	return c
}

// Options allows to configure formatter behavior
func (c *formatter) Options(ops ...xlog.FormatterOption) xlog.Formatter {
	c.Apply(ops...)
	c.Rebind(c.Sink())
	return c
}

// FormatKV log entry string to the stream,
// the entries are key/value pairs
func (c *formatter) FormatKV(pkg string, level xlog.LogLevel, depth int, entries ...any) {
	obj := &kventries{
		printEmpty: c.PrintEmpty,
		entries:    entries,
	}
	c.format(pkg, level, depth+1, obj)
}

// Format log entry string to the stream
func (c *formatter) Format(pkg string, l xlog.LogLevel, depth int, entries ...any) {
	c.format(pkg, l, depth+1, nil, entries...)
}

func (c *formatter) format(pkg string, l xlog.LogLevel, depth int, obj *kventries, entries ...any) {
	ee := c.prepareEntry(pkg, l, depth+1, obj, entries...)
	c.writeEntry(ee)
}

func (c *formatter) prepareEntry(pkg string, l xlog.LogLevel, depth int, obj *kventries, entries ...any) entry {
	severity := levelsToSeverity[l]
	if severity == "" {
		severity = severityInfo
	}

	if obj == nil {
		obj = &kventries{
			printEmpty: c.PrintEmpty,
		}
	}

	if len(entries) > 0 {
		str := fmt.Sprint(entries...)
		if len(str) > MaxLogMessageLength {
			str = str[:MaxLogMessageLength] + "..."
		}
		obj.entries = append(obj.entries, "msg", str)
	}

	fn, file, line := callerName(depth + 1)
	ee := entry{
		LogName:     c.logName,
		Component:   pkg,
		Severity:    severity,
		JSONPayload: obj,
		Source: &reportLocation{
			Function: fn,
		},
	}

	if !c.SkipTime {
		ee.Time = xlog.TimeNowFn().UTC().Format(time.RFC3339)
	}

	if c.WithCaller {
		if c.WithLocation || l <= xlog.ERROR {
			ee.Source.FilePath = path.Base(file)
			ee.Source.LineNumber = line
		}
	}

	return ee
}

func (c *formatter) writeEntry(ee entry) {
	// One marshal per record: the payload marshaler runs inside it, so no value
	// is encoded twice on the way to a sink. Marshal before taking a buffer so a
	// panicking marshaler cannot strand one.
	b, err := json.Marshal(ee)
	if err != nil {
		c.RecordError(errors.WithMessage(err, "unable to encode Stackdriver log record"))
		return
	}
	record := c.Buffer()
	buffer := record.Buffer()
	_, _ = buffer.Write(b)
	_ = buffer.WriteByte('\n')
	c.Emit(record)
}

type entry struct {
	LogName     string          `json:"logName,omitempty"`
	Component   string          `json:"component,omitempty"`
	Time        string          `json:"timestamp,omitempty"`
	JSONPayload any             `json:"message,omitempty"`
	Severity    severity        `json:"severity,omitempty"`
	Source      *reportLocation `json:"sourceLocation,omitempty"`
}

type reportLocation struct {
	FilePath   string `json:"file,omitempty"`
	LineNumber int    `json:"line,omitempty"`
	Function   string `json:"function,omitempty"`
}

// String returns JSON without HTML escaping. Errors without json.Marshaler
// support use their detailed text. Encoding failures are ignored and return
// an empty string. This helper is separate from the formatter's KV encoding.
func String(value any) string {
	if err, ok := value.(error); ok {
		// if error does not support json.Marshaler,
		// the print the full details
		if _, ok := value.(json.Marshaler); !ok {
			value = fmt.Sprintf("%+v", err)
		}
	}
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	return strings.TrimSpace(buffer.String())
}

func callerName(depth int) (string, string, int) {
	pc, file, line, ok := runtime.Caller(depth)
	details := runtime.FuncForPC(pc)
	if ok && details != nil {
		name := path.Base(details.Name())
		name = removePart(name, "[", "]")
		name = removePart(name, "(", ")")

		idx := strings.Index(name, ".")
		if idx >= 0 {
			name = strings.TrimLeft(name[idx+1:], ".")
		}

		return name, file, line
	}
	return "n/a", file, line
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

type kventries struct {
	entries    []any
	printEmpty bool
}

func (o *kventries) MarshalJSON() (out []byte, err error) {
	if len(o.entries) == 0 {
		return []byte(`{}`), nil
	}

	out = append(out, '{')

	size := len(o.entries)
	lastComma := false

	for i := 0; i < size; i += 2 {
		k, ok := o.entries[i].(string)
		if !ok {
			panic(fmt.Sprintf("key is not a string: %s", String(o.entries[i])))
		}
		var v any
		if i+1 < size {
			v = o.entries[i+1]
		}
		if v == nil && !o.printEmpty {
			continue
		}
		if s, ok := v.(string); ok && s == "" && !o.printEmpty {
			continue
		}

		key, err := json.Marshal(k)
		if err != nil {
			return nil, errors.WithMessage(err, "unable to encode Stackdriver field key")
		}
		// JSON marshalers retain ownership of their representation. Display-only
		// values become JSON strings; ordinary numbers keep their exact JSON value.
		if _, marshaler := v.(json.Marshaler); !marshaler {
			switch value := v.(type) {
			case error:
				v = fmt.Sprintf("%+v", value)
			case xlog.WithValueString:
				v = value.ValueString()
			case json.Number, *json.Number:
				// Preserve encoding/json's numeric representation.
			case fmt.Stringer:
				v = value.String()
			}
		}
		val, err := json.Marshal(v)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to encode Stackdriver field %s", k)
		}
		out = append(out, key...)
		out = append(out, ':')
		out = append(out, val...)
		out = append(out, ',')
		lastComma = true
	}
	if lastComma {
		// replace last ',' with '}'
		out[len(out)-1] = '}'
	} else {
		out = append(out, '}')
	}
	return out, nil
}
