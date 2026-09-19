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
	"fmt"
	"io"
	"time"

	"github.com/cockroachdb/errors"
)

// NewJSONFormatter returns a JSONFormatter writing one JSON object per record
// to w. Options apply before the destination is bound, so WithSink takes effect
// immediately.
func NewJSONFormatter(w io.Writer, ops ...FormatterOption) Formatter {
	f := &JSONFormatter{}
	f.WithCaller = true
	f.MaxLogLength = DefaultMaxLogMessageLength
	f.Apply(ops...)
	f.Bind(w, f.sink)
	return f
}

// JSONFormatter formats log entries as JSON objects. Do not copy it after first use.
type JSONFormatter struct {
	Config
	Output
}

// Options allows to configure formatter behavior
func (c *JSONFormatter) Options(ops ...FormatterOption) Formatter {
	c.Apply(ops...)
	c.Rebind(c.sink)
	return c
}

// FormatKV log entry string to the stream,
// the entries are key/value pairs
func (c *JSONFormatter) FormatKV(pkg string, l LogLevel, depth int, entries ...any) {
	m := kvToMap(entries...)
	c.format(pkg, l, depth+1, m)
}

// Format log entry string to the stream
func (c *JSONFormatter) Format(pkg string, l LogLevel, depth int, entries ...any) {
	c.format(pkg, l, depth+1, map[string]any{}, entries...)
}

// Format log entry string to the stream
func (c *JSONFormatter) format(pkg string, l LogLevel, depth int, kv map[string]any, entries ...any) {
	if !c.SkipTime {
		now := TimeNowFn().UTC()
		kv["time"] = now.Format(time.RFC3339)
	}
	if !c.SkipLevel {
		kv["level"] = l.Char()
	}
	if pkg != "" {
		kv["pkg"] = pkg
	}

	if l == ERROR || c.WithLocation || c.WithCaller {
		caller, file, line := Caller(depth + 1)
		if l == ERROR || c.WithLocation {
			kv["src"] = fmt.Sprintf("%s:%d", file, line)
		}
		if l == ERROR || c.WithCaller {
			kv["func"] = caller
		}
	}

	if len(entries) > 0 {
		msg := fmt.Sprint(entries...)
		if c.MaxLogLength > 0 && len(msg) > c.MaxLogLength {
			msg = msg[:c.MaxLogLength]
		}
		kv["msg"] = msg
	}

	record := c.Buffer()
	if err := record.Encoder().Encode(kv); err != nil {
		// Reject the whole record instead of emitting a partial JSON line.
		c.Discard(record)
		c.RecordError(errors.WithMessage(err, "unable to encode JSON log record"))
		return
	}
	c.Emit(record, l)
}

func kvToMap(kvList ...any) map[string]any {
	size := len(kvList)
	m := make(map[string]any)

	for i := 0; i < size; i += 2 {
		k, ok := kvList[i].(string)
		if !ok {
			panic(fmt.Sprintf("key is not a string: %v", EscapedString(kvList[i])))
		}
		var v any
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
