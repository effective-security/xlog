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
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// NewJSONFormatter returns an instance of JsonFormatter
func NewJSONFormatter(w io.Writer) Formatter {
	return &JSONFormatter{
		w: bufio.NewWriter(w),
		config: config{
			withCaller:   true,
			skipTime:     false,
			withLocation: false,
			color:        false,
		},
	}
}

// JSONFormatter provides default logs format
type JSONFormatter struct {
	config
	w *bufio.Writer
}

// Options allows to configure formatter behavior
func (c *JSONFormatter) Options(ops ...FormatterOption) Formatter {
	c.config.options(ops)
	return c
}

// FormatKV log entry string to the stream,
// the entries are key/value pairs
func (c *JSONFormatter) FormatKV(pkg string, l LogLevel, depth int, entries ...interface{}) {
	m := kvToMap(entries...)
	c.format(pkg, l, depth+1, false, m)
}

// Format log entry string to the stream
func (c *JSONFormatter) Format(pkg string, l LogLevel, depth int, entries ...interface{}) {
	c.format(pkg, l, depth+1, true, map[string]interface{}{}, entries...)
}

// Format log entry string to the stream
func (c *JSONFormatter) format(pkg string, l LogLevel, depth int, escape bool, kv map[string]interface{}, entries ...interface{}) {
	if !c.skipTime {
		now := TimeNowFn().UTC()
		kv["time"] = now.Format(time.RFC3339)
	}
	if !c.skipLevel {
		kv["level"] = l.Char()
	}
	if pkg != "" {
		kv["pkg"] = pkg
	}

	if l == ERROR || c.withLocation || c.withCaller {
		caller, file, line := Caller(depth + 1)
		if l == ERROR || c.withLocation {
			kv["src"] = fmt.Sprintf("%s:%d", file, line)
		}
		if l == ERROR || c.withCaller {
			kv["func"] = caller
		}
	}

	if len(entries) > 0 {
		msg := fmt.Sprint(entries...)
		if len(msg) > 1024 {
			msg = msg[:1024] + "...\""
		}
		kv["msg"] = msg
	}

	encoder := json.NewEncoder(c.w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(kv)

	c.Flush()
}

// Flush the logs
func (c *JSONFormatter) Flush() {
	c.w.Flush()
}

func kvToMap(kvList ...interface{}) map[string]interface{} {
	size := len(kvList)
	m := make(map[string]interface{})

	for i := 0; i < size; i += 2 {
		k, ok := kvList[i].(string)
		if !ok {
			panic(fmt.Sprintf("key is not a string: %v", EscapedString(kvList[i])))
		}
		var v interface{}
		if i+1 < size {
			v = kvList[i+1]
		}
		switch typ := v.(type) {
		case error:
			v = fmt.Sprintf("%+v", typ)
		}
		m[k] = v
	}
	return m
}
