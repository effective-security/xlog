package xlog_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/xlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatterDeliveryErrors(t *testing.T) {
	constructors := []struct {
		name string
		new  func(io.Writer) xlog.Formatter
	}{
		{"string", xlog.NewStringFormatter},
		{"pretty", xlog.NewPrettyFormatter},
		{"json", xlog.NewJSONFormatter},
	}
	writeErr := errors.New("destination unavailable")
	for _, constructor := range constructors {
		t.Run(constructor.name, func(t *testing.T) {
			calls := 0
			shortWriter := callbackWriter(func([]byte) (int, error) {
				calls++
				if calls == 1 {
					return 0, nil
				}
				// Bound a regression: bufio used to retry the failed large write.
				return 0, io.ErrUnexpectedEOF
			})
			large := constructor.new(shortWriter).(xlog.ErrorFormatter)
			large.Format("test", xlog.INFO, 0, strings.Repeat("x", 16<<10))
			require.ErrorIs(t, large.Err(), io.ErrShortWrite)
			require.Equal(t, 1, calls)
			for _, failure := range []struct {
				name string
				err  error
			}{
				{"write", writeErr},
				{"short-write", io.ErrShortWrite},
			} {
				t.Run(failure.name, func(t *testing.T) {
					writer := callbackWriter(func([]byte) (int, error) {
						if failure.err == io.ErrShortWrite {
							return 0, nil
						}
						return 0, failure.err
					})
					formatter := constructor.new(writer).(xlog.ErrorFormatter)
					formatter.FormatKV("test", xlog.INFO, 0, "message", "hello")
					require.ErrorIs(t, formatter.Err(), failure.err)
					require.ErrorIs(t, xlog.FlushFormatter(formatter), failure.err)
				})
			}
			var out bytes.Buffer
			// Hide the concrete type so bufio.NewWriter does not reuse the sink.
			downstream := struct{ *bufio.Writer }{bufio.NewWriter(&out)}
			formatter := constructor.new(downstream)
			formatter.Format("test", xlog.INFO, 0, "hello")
			require.Empty(t, out.String(), "normal formatting retains downstream buffering")
			require.NoError(t, xlog.FlushFormatter(formatter))
			require.Contains(t, out.String(), "hello")
		})
	}
}

func TestJSONFormatterEncodingErrors(t *testing.T) {
	var out bytes.Buffer
	f := xlog.NewJSONFormatter(&out).(xlog.ErrorFormatter)
	f.FormatKV("test", xlog.INFO, 0, "unsupported", make(chan int))
	var unsupported *json.UnsupportedTypeError
	require.ErrorAs(t, f.Err(), &unsupported)
	require.Empty(t, out.String())
	first := f.Err()
	f.FormatKV("test", xlog.INFO, 0, "valid", "hello")
	require.Contains(t, out.String(), `"valid":"hello"`)
	assert.Same(t, first, f.Err(), "successful records must not erase evidence of loss")
	require.NoError(t, xlog.FlushFormatter(nil))
	require.NoError(t, xlog.FlushFormatter(xlog.NewNilFormatter()))
}
