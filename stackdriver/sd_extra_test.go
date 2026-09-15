package stackdriver_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/xlog"
	"github.com/effective-security/xlog/stackdriver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type enum int

func (enum) ValueString() string { return "enum-value" }

type marshaledError struct{}

func (marshaledError) Error() string                { return "display" }
func (marshaledError) MarshalJSON() ([]byte, error) { return []byte(`{"custom":true}`), nil }

func TestStackdriverJSONValues(t *testing.T) {
	stamp := time.Date(2026, 9, 15, 12, 0, 0, 123, time.UTC)
	plainError := errors.New("failure")
	for _, tc := range []struct {
		name  string
		value any
		want  any
	}{
		{"plain-string", "hello", "hello"},
		{"boolean-string", "true", "true"},
		{"number-string", "123", "123"},
		{"null-string", "null", "null"},
		{"whitespace", "  hello\n", "  hello\n"},
		{"quoted", `"hello"`, `"hello"`},
		{"empty", "", ""},
		{"nil", nil, nil},
		{"bool", true, true},
		{"duration", time.Second, "1s"},
		{"time", stamp, stamp.Format(time.RFC3339Nano)},
		{"enum", enum(1), "enum-value"},
		{"uint64", uint64(math.MaxUint64), json.Number("18446744073709551615")},
		{"int64", int64(math.MinInt64), json.Number("-9223372036854775808")},
		{"raw-json", json.RawMessage(`{"nested":true}`), map[string]any{"nested": true}},
		{"error", plainError, fmt.Sprintf("%+v", plainError)},
		{"marshaled-error", marshaledError{}, map[string]any{"custom": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			f := stackdriver.NewFormatter(&out, "test").Options(xlog.FormatPrintEmpty(true)).(xlog.ErrorFormatter)
			f.FormatKV("test", xlog.INFO, 0, "value", tc.value)
			require.NoError(t, f.Err())
			var record struct {
				Message map[string]any `json:"message"`
			}
			decoder := json.NewDecoder(&out)
			decoder.UseNumber()
			require.NoError(t, decoder.Decode(&record))
			actual, exists := record.Message["value"]
			require.True(t, exists)
			assert.Equal(t, tc.want, actual)
		})
	}
}

func TestStackdriverMessagesAndEmptyValues(t *testing.T) {
	var out bytes.Buffer
	f := stackdriver.NewFormatter(&out, "test").(xlog.ErrorFormatter)
	f.Format("test", xlog.INFO, 0, "hello")
	require.NoError(t, f.Err())
	require.Contains(t, out.String(), `"msg":"hello"`)
	out.Reset()
	f.FormatKV("test", xlog.INFO, 0, "empty", "", "nil", nil, "false", false, "zero", 0)
	var record struct {
		Message map[string]any `json:"message"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &record))
	require.Equal(t, map[string]any{"false": false, "zero": float64(0)}, record.Message)
}

func TestStackdriverEncodingErrors(t *testing.T) {
	for _, value := range []any{make(chan int), json.RawMessage(`invalid`), math.NaN()} {
		var out bytes.Buffer
		f := stackdriver.NewFormatter(&out, "test").(xlog.ErrorFormatter)
		f.FormatKV("test", xlog.INFO, 0, "bad", value)
		require.Error(t, f.Err())
		var marshalerErr *json.MarshalerError
		require.ErrorAs(t, f.Err(), &marshalerErr)
		require.Empty(t, out.String())
		first := f.Err()
		f.FormatKV("test", xlog.INFO, 0, "good", "hello")
		require.Contains(t, out.String(), `"good":"hello"`)
		require.Same(t, first, f.Err())
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

type shortThenErrorWriter struct{ calls int }

func (w *shortThenErrorWriter) Write([]byte) (int, error) {
	w.calls++
	if w.calls == 1 {
		return 0, nil
	}
	return 0, io.ErrUnexpectedEOF
}

func TestStackdriverWriteErrors(t *testing.T) {
	writer := &shortThenErrorWriter{}
	f := stackdriver.NewFormatter(writer, "test").(xlog.ErrorFormatter)
	f.FormatKV("test", xlog.INFO, 0, "large", strings.Repeat("x", 16<<10))
	require.ErrorIs(t, f.Err(), io.ErrShortWrite)
	require.Equal(t, 1, writer.calls)
	sentinel := errors.New("write failed")
	for _, tc := range []struct{ returned, want error }{
		{sentinel, sentinel},
		{nil, io.ErrShortWrite},
	} {
		f := stackdriver.NewFormatter(failingWriter{err: tc.returned}, "test").(xlog.ErrorFormatter)
		f.Format("test", xlog.INFO, 0, "hello")
		require.ErrorIs(t, f.Err(), tc.want)
		require.ErrorIs(t, xlog.FlushFormatter(f), tc.want)
	}
}
