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
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"runtime"
	"strings"
	"time"

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
	severityAlert    severity = "ALERT"
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

// formatter provides logs format for StackDriver
type formatter struct {
	w          *bufio.Writer
	logName    string
	withCaller bool
}

// NewFormatter returns an instance of StackdriverFormatter
func NewFormatter(w io.Writer, logName string) xlog.Formatter {
	return &formatter{
		w:          bufio.NewWriter(w),
		logName:    logName,
		withCaller: true,
	}
}

// WithCaller allows to configure if the caller shall be logged
func (c *formatter) WithCaller(val bool) xlog.Formatter {
	c.withCaller = val
	return c
}

// FormatKV log entry string to the stream,
// the entries are key/value pairs
func (c *formatter) FormatKV(pkg string, level xlog.LogLevel, depth int, entries ...interface{}) {
	c.Format(pkg, level, depth+1, flatten(entries...))
}

// Format log entry string to the stream
func (c *formatter) Format(pkg string, l xlog.LogLevel, depth int, entries ...interface{}) {
	severity := levelsToSeverity[l]
	if severity == "" {
		severity = severityInfo
	}

	fn, file, line := callerName(depth + 1)

	str := "src=" + fn + ", " + fmt.Sprint(entries...)
	ee := entry{
		LogName:   c.logName,
		Component: pkg,
		Time:      time.Now().UTC().Format(time.RFC3339),
		Message:   str,
		Severity:  severity,
		Source: &reportLocation{
			Function: fn,
		},
	}

	if l <= xlog.ERROR {
		ee.Source.FilePath = path.Base(file)
		ee.Source.LineNumber = line
	}

	b, err := json.Marshal(ee)
	if err == nil {
		c.w.Write(b)
		c.w.WriteByte('\n')
	}

	c.Flush()
}

// Flush the logs
func (c *formatter) Flush() {
	c.w.Flush()
}

type entry struct {
	LogName   string          `json:"logName,omitempty"`
	Component string          `json:"component,omitempty"`
	Time      string          `json:"timestamp,omitempty"`
	Message   string          `json:"textPayload,omitempty"`
	Severity  severity        `json:"severity,omitempty"`
	Source    *reportLocation `json:"sourceLocation,omitempty"`
}

type reportLocation struct {
	FilePath   string `json:"file,omitempty"`
	LineNumber int    `json:"line,omitempty"`
	Function   string `json:"function,omitempty"`
}

func flatten(kvList ...interface{}) string {
	size := len(kvList)
	buf := bytes.Buffer{}
	for i := 0; i < size; i += 2 {
		k, ok := kvList[i].(string)
		if !ok {
			panic(fmt.Sprintf("key is not a string: %s", String(kvList[i])))
		}
		var v interface{}
		if i+1 < size {
			v = kvList[i+1]
		}
		if i > 0 {
			buf.WriteRune(',')
			buf.WriteRune(' ')
		}
		buf.WriteString(k)
		buf.WriteString("=")
		buf.WriteString(String(v))

	}
	return buf.String()
}

// String returns string value stuitable for logging
func String(value interface{}) string {
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
	encoder.Encode(value)
	return strings.TrimSpace(buffer.String())
}

func callerName(depth int) (string, string, int) {
	pc, file, line, ok := runtime.Caller(depth)
	details := runtime.FuncForPC(pc)
	if ok && details != nil {
		name := path.Base(details.Name())

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
	return "n/a", file, line
}
