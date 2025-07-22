package xlog

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlatten(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		printEmpty bool
		kvList     []any
		want       []any
		wantPanic  bool
	}{
		{
			name:       "key-value pairs",
			printEmpty: false,
			kvList:     []any{"k1", "v1", "k2", nil},
			want:       []any{"k1=\"v1\""},
		},
		{
			name:       "print empty value",
			printEmpty: true,
			kvList:     []any{"k1", "v1", "k2", nil},
			want:       []any{"k1=\"v1\"", "k2=null"},
		},
		{
			name:      "panic on non-string key",
			wantPanic: true,
			kvList:    []any{123, "v"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.wantPanic {
				require.Panics(t, func() {
					flatten(tc.printEmpty, tc.kvList...)
				})
			} else {
				got := flatten(tc.printEmpty, tc.kvList...)
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func TestRemovePart(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, val, open, close, want string
	}{
		{"no markers", "abcdef", "[", "]", "abcdef"},
		{"strip markers", "a[b]c", "[", "]", "ac"},
		{"unclosed", "x[y", "[", "]", "x"},
		{"closing only", "x]y", "[", "]", "x]y"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := removePart(tc.val, tc.open, tc.close)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCaller(t *testing.T) {
	t.Parallel()
	name, file, line := Caller(0)
	assert.Equal(t, "Caller", name)
	assert.Equal(t, "formatters.go", file)
	require.Greater(t, line, 0)

	name, file, line = Caller(1000)
	assert.Equal(t, "func", name)
	assert.Equal(t, "???", file)
	assert.Equal(t, 1, line)
}

func TestKVToMap(t *testing.T) {
	t.Parallel()
	m := kvToMap("a", 1, "b", "two")
	want := map[string]any{"a": 1, "b": "two"}
	assert.Equal(t, want, m)
	require.Panics(t, func() {
		kvToMap(1, 2, "c", 3)
	})
}

func TestNewDefaultFormatter(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	df := NewDefaultFormatter(&buf)
	pf := NewPrettyFormatter(&buf)
	require.NotNil(t, df)
	assert.Equal(t, reflect.TypeOf(pf), reflect.TypeOf(df))
}
