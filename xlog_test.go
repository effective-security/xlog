// Copyright 2018, Denis Issoupov
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
package xlog_test

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/effective-security/xlog"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var logger = xlog.NewPackageLogger("github.com/effective-security/xlog", "xlog_test")

const logPrefixFormt = "2018-04-17 20:53:46.589926 "

// NOTE: keep the xxxError() functions at the beginnign of the file,
// as tests produce the error stack

func originateError(errmsg string, level int) error {
	return errors.Errorf("originateError: msg=%s, level=%d", errmsg, level)
}

func traceError(errmsg string, levels int) error {
	if levels > 0 {
		return errors.WithStack(traceError(errmsg, levels-1))
	}
	return errors.WithStack(originateError(errmsg, 0))
}

func annotateError(errmsg string, levels int) error {
	if levels > 0 {
		return errors.WithStack(annotateError(errmsg, levels-1))
	}
	return errors.WithMessagef(originateError(errmsg, 0), "annotateError, level=%d", levels)
}

func withTracedError(errmsg string, levels int) error {
	return traceError(errmsg, levels)
}

func withAnnotateError(errmsg string, levels int) error {
	return annotateError(errmsg, levels)
}

func Test_NewLogger(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	xlog.SetGlobalLogLevel(xlog.INFO)
	xlog.SetFormatter(xlog.NewPrettyFormatter(writer))
	logger.Infof("Info log")
	logger.Errorf("Error log")
	logger.Noticef("Notice log")
	logger.Log(xlog.INFO, "log log")
	logger.Logf(xlog.INFO, "log %s", "log")

	assert.Equal(t, "2021-04-01 00:00:00.000000 I | pkg=xlog_test, func=Test_NewLogger, \"Info log\"\n2021-04-01 00:00:00.000000 E | pkg=xlog_test, func=Test_NewLogger, \"Error log\"\n2021-04-01 00:00:00.000000 N | pkg=xlog_test, func=Test_NewLogger, \"Notice log\"\n2021-04-01 00:00:00.000000 I | pkg=xlog_test, func=Test_NewLogger, \"log log\"\n2021-04-01 00:00:00.000000 I | pkg=xlog_test, func=Test_NewLogger, \"log log\"\n", b.String())

	b.Reset()
	xlog.GetFormatter().Options(xlog.FormatNoCaller)
	logger.Infof("Info log")
	logger.Errorf("Error log")
	logger.Noticef("Notice log")
	logger.Log(xlog.INFO, "log log")
	logger.Logf(xlog.INFO, "log %s", "log")

	assert.Equal(t, "2021-04-01 00:00:00.000000 I | pkg=xlog_test, \"Info log\"\n2021-04-01 00:00:00.000000 E | pkg=xlog_test, \"Error log\"\n2021-04-01 00:00:00.000000 N | pkg=xlog_test, \"Notice log\"\n2021-04-01 00:00:00.000000 I | pkg=xlog_test, \"log log\"\n2021-04-01 00:00:00.000000 I | pkg=xlog_test, \"log log\"\n", b.String())
}

func Test_PrettyFormatter(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	xlog.SetGlobalLogLevel(xlog.INFO)
	xlog.SetFormatter(xlog.NewPrettyFormatter(writer))

	logger.Info("Test Info")
	expected := "2021-04-01 00:00:00.000000 I | pkg=xlog_test, func=Test_PrettyFormatter, \"Test Info\"\n"
	assert.Equal(t, expected, b.String())
	b.Reset()

	logger.Infof("Test Infof")
	expected = "2021-04-01 00:00:00.000000 I | pkg=xlog_test, func=Test_PrettyFormatter, \"Test Infof\"\n"
	assert.Equal(t, expected, b.String())
	b.Reset()

	k3 := struct {
		Foo string
	}{Foo: "bar"}

	logger.KV(xlog.INFO, "k1", 1, "k2", false, "k3", k3)
	expected = "2021-04-01 00:00:00.000000 I | pkg=xlog_test, func=Test_PrettyFormatter, k1=1, k2=false, k3={\"Foo\":\"bar\"}\n"
	assert.Equal(t, expected, b.String())
	b.Reset()

	logger.Errorf("Test Error")
	expected = "2021-04-01 00:00:00.000000 E | pkg=xlog_test, func=Test_PrettyFormatter, \"Test Error\"\n"
	assert.Equal(t, expected, b.String())
	b.Reset()

	logger.Warningf("Test Warning")
	expected = "2021-04-01 00:00:00.000000 W | pkg=xlog_test, func=Test_PrettyFormatter, \"Test Warning\"\n"
	assert.Equal(t, expected, b.String())
	b.Reset()

	// Debug level is disabled
	logger.Debugf("Test Debug")
	expected = ""
	assert.Equal(t, expected, b.String())
	b.Reset()

	xlog.SetGlobalLogLevel(xlog.DEBUG)
	logger.Debugf("Test Debug")
	expected = "2021-04-01 00:00:00.000000 D | pkg=xlog_test, func=Test_PrettyFormatter, \"Test Debug\"\n"
	assert.Equal(t, expected, b.String())
	b.Reset()
}

func Test_WithTracedError(t *testing.T) {
	wd, err := os.Getwd() // package dir
	require.NoError(t, err)

	cases := []struct {
		msg           string
		levels        int
		expectedErr   string
		expectedStack string
	}{
		{
			"Test_WithTracedError(1)",
			1,
			"E | pkg=xlog_test, \"err=[originateError: msg=Test_WithTracedError(1), level=0]\"\n",
			"E | pkg=xlog_test, \"stack=[originateError: msg=Test_WithTracedError(1), level=0\\ngithub.com/effective-security/xlog_test.originateError\\n\\tgithub.com/effective-security/xlog/xlog_test.go:39\\ngithub.com/effective-security/xlog_test.traceError\\n\\tgithub.com/",
		},
		{
			"Test_WithTracedError(4)",
			2,
			"E | pkg=xlog_test, \"err=[originateError: msg=Test_WithTracedError(4), level=0]\"\n",
			"E | pkg=xlog_test, \"stack=[originateError: msg=Test_WithTracedError(4), level=0\\ngithub.com/effective-security/xlog_test.originateError\\n\\tgithub.com/effective-security/xlog/xlog_test.go:39\\ngithub.com/effective-security/xlog_test.traceError\\n\\tgithub.com/",
		},
	}

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	xlog.SetFormatter(xlog.NewPrettyFormatter(writer).Options(xlog.FormatNoCaller))

	prefixLen := len(logPrefixFormt)
	for idx, c := range cases {
		err := withTracedError(c.msg, c.levels)
		require.Error(t, err)

		logger.Errorf("err=[%v]", err)
		result := b.String()[prefixLen:]
		assert.Equal(t, c.expectedErr, result, "[%d] case failed expectation", idx)
		b.Reset()

		logger.Errorf("err=[%v]", err.Error())
		result = b.String()[prefixLen:]
		assert.Equal(t, c.expectedErr, result, "[%d] case failed expectation", idx)
		b.Reset()

		logger.Errorf("stack=[%+v]", err)
		result = b.String()[prefixLen:]
		// remove paths from the trace
		result = strings.Replace(result, wd, "github.com/effective-security/xlog", -1)
		assert.Equal(t, c.expectedStack[:256], result[:256], "[%d] case failed expectation", idx)
		b.Reset()
	}
}

func Test_WithAnnotatedError(t *testing.T) {
	wd, _ := os.Getwd() // package dir

	cases := []struct {
		msg           string
		levels        int
		expectedErr   string
		expectedStack string
	}{
		{
			"Test_WithAnnotatedError(1)",
			1,
			"E | pkg=xlog_test, \"err=[annotateError, level=0: originateError: msg=Test_WithAnnotatedError(1), level=0]\"\n",
			"E | pkg=xlog_test, \"stack=[originateError: msg=Test_WithAnnotatedError(1), level=0\\ngithub.com/effective-security/xlog_test.originateError\\n\\tgithub.com/effective-security/xlog/xlog_test.go:39\\ngithub.com/effective-security/xlog_test.annotateError\\n\\tgithu",
		},
		{
			"Test_WithAnnotatedError(4)",
			2,
			"E | pkg=xlog_test, \"err=[annotateError, level=0: originateError: msg=Test_WithAnnotatedError(4), level=0]\"\n",
			"E | pkg=xlog_test, \"stack=[originateError: msg=Test_WithAnnotatedError(4), level=0\\ngithub.com/effective-security/xlog_test.originateError\\n\\tgithub.com/effective-security/xlog/xlog_test.go:39\\ngithub.com/effective-security/xlog_test.annotateError\\n\\tgithu",
		},
	}

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	xlog.SetFormatter(xlog.NewPrettyFormatter(writer).Options(xlog.FormatNoCaller))

	prefixLen := len(logPrefixFormt)
	for idx, c := range cases {
		err := withAnnotateError(c.msg, c.levels)
		require.Error(t, err)

		logger.Errorf("err=[%v]", err)
		result := b.String()[prefixLen:]
		assert.Equal(t, c.expectedErr, result, "[%d] case failed expectation", idx)
		b.Reset()

		logger.Errorf("err=[%v]", err.Error())
		result = b.String()[prefixLen:]
		assert.Equal(t, c.expectedErr, result, "[%d] case failed expectation", idx)
		b.Reset()

		logger.Errorf("stack=[%+v]", err)
		result = b.String()[prefixLen:]
		// remove paths from the trace
		result = strings.Replace(result, wd, "github.com/effective-security/xlog", -1)
		assert.Equal(t, c.expectedStack[:256], result[:256], "[%d] case failed expectation", idx)
		b.Reset()
	}
}

func Test_LevelAt(t *testing.T) {
	l, err := xlog.GetRepoLogger("github.com/effective-security/xlog")
	require.NoError(t, err)

	l.SetRepoLogLevel(xlog.INFO)
	assert.True(t, logger.LevelAt(xlog.INFO))
	assert.False(t, logger.LevelAt(xlog.TRACE))
	assert.False(t, logger.LevelAt(xlog.DEBUG))

	l.SetRepoLogLevel(xlog.TRACE)
	assert.True(t, logger.LevelAt(xlog.INFO))
	assert.True(t, logger.LevelAt(xlog.TRACE))
	assert.False(t, logger.LevelAt(xlog.DEBUG))

	l.SetRepoLogLevel(xlog.DEBUG)
	assert.True(t, logger.LevelAt(xlog.INFO))
	assert.True(t, logger.LevelAt(xlog.TRACE))
	assert.True(t, logger.LevelAt(xlog.DEBUG))
}

func Test_PrettyFormatterDebug(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	xlog.SetFormatter(xlog.NewPrettyFormatter(writer).Options(xlog.FormatWithCaller))
	xlog.SetGlobalLogLevel(xlog.INFO)

	logger.Trace("Test trace")
	logger.Tracef("Test tracef")
	result := b.String()
	expected := ""
	assert.Equal(t, expected, result)
	b.Reset()

	logger.Info("Test Info")
	logger.Infof("Test Infof")
	result = b.String()
	expected = "2021-04-01 00:00:00.000000 I | pkg=xlog_test, func=Test_PrettyFormatterDebug, \"Test Info\"\n2021-04-01 00:00:00.000000 I | pkg=xlog_test, func=Test_PrettyFormatterDebug, \"Test Infof\"\n"
	assert.Equal(t, expected, result)
	b.Reset()

	logger.KV(xlog.INFO, "k1", 1, "k2", false)
	writer.Flush()
	result = b.String()
	expected = "2021-04-01 00:00:00.000000 I | pkg=xlog_test, func=Test_PrettyFormatterDebug, k1=1, k2=false\n"
	assert.Equal(t, expected, result)
	b.Reset()

	logger.Error("Test Error")
	logger.Errorf("Test Errorf")
	result = b.String()
	expected = "2021-04-01 00:00:00.000000 E | pkg=xlog_test, func=Test_PrettyFormatterDebug, \"Test Error\"\n2021-04-01 00:00:00.000000 E | pkg=xlog_test, func=Test_PrettyFormatterDebug, \"Test Errorf\"\n"
	assert.Equal(t, expected, result)
	b.Reset()

	logger.Notice("Test Notice")
	logger.Noticef("Test Noticef")
	result = b.String()
	expected = "2021-04-01 00:00:00.000000 N | pkg=xlog_test, func=Test_PrettyFormatterDebug, \"Test Notice\"\n2021-04-01 00:00:00.000000 N | pkg=xlog_test, func=Test_PrettyFormatterDebug, \"Test Noticef\"\n"
	assert.Equal(t, expected, result)
	b.Reset()

	logger.Warning("Test Warning")
	logger.Warningf("Test Warning")
	result = b.String()
	expected = "2021-04-01 00:00:00.000000 W | pkg=xlog_test, func=Test_PrettyFormatterDebug, \"Test Warning\"\n2021-04-01 00:00:00.000000 W | pkg=xlog_test, func=Test_PrettyFormatterDebug, \"Test Warning\"\n"
	assert.Equal(t, expected, result)
	b.Reset()

	// Debug level is disabled
	logger.Debug("Test Debug")
	logger.Debugf("Test Debug")
	result = b.String()
	expected = ""
	assert.Equal(t, expected, result)
	b.Reset()

	xlog.SetGlobalLogLevel(xlog.DEBUG)
	logger.Debug("Test Debug")
	logger.Debugf("Test Debug")
	result = b.String()
	expected = "2021-04-01 00:00:00.000000 D | pkg=xlog_test, func=Test_PrettyFormatterDebug, \"Test Debug\"\n2021-04-01 00:00:00.000000 D | pkg=xlog_test, func=Test_PrettyFormatterDebug, \"Test Debug\"\n"
	assert.Equal(t, expected, result)
	b.Reset()

	xlog.SetGlobalLogLevel(xlog.TRACE)

	logger.Trace("Test trace")
	logger.Tracef("Test trace")
	result = b.String()
	expected = "2021-04-01 00:00:00.000000 T | pkg=xlog_test, func=Test_PrettyFormatterDebug, \"Test trace\"\n2021-04-01 00:00:00.000000 T | pkg=xlog_test, func=Test_PrettyFormatterDebug, \"Test trace\"\n"
	assert.Equal(t, expected, result)
	b.Reset()

	logger.Flush()
}

func Test_StringFormatter(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	xlog.SetFormatter(xlog.NewStringFormatter(writer).Options(xlog.FormatWithCaller))
	xlog.SetGlobalLogLevel(xlog.INFO)

	func() {
		logger.Infof("Test Info")
	}()
	result := b.String()
	assert.Equal(t, "time=2021-04-01T00:00:00Z level=I pkg=xlog_test func=Test_StringFormatter.func1 \"Test Info\"\n", result)
	b.Reset()

	func() {
		s := someSvc{}
		s.log("Test Info")
	}()
	result = b.String()
	assert.Equal(t, "time=2021-04-01T00:00:00Z level=I pkg=xlog_test func=log \"Test Info\"\n", result)
	b.Reset()

	logger.Errorf("Test Error")
	result = b.String()
	assert.Equal(t, "time=2021-04-01T00:00:00Z level=E pkg=xlog_test func=Test_StringFormatter \"Test Error\"\n", result)
	b.Reset()

	logger.Warningf("Test Warning")
	result = b.String()
	assert.Equal(t, "time=2021-04-01T00:00:00Z level=W pkg=xlog_test func=Test_StringFormatter \"Test Warning\"\n", result)
	b.Reset()

	// Debug level is disabled
	logger.Debugf("Test Debug")
	result = b.String()
	assert.Contains(t, "time=2021-04-01T00:00:00Z level=E pkg=xlog_test func=Test_StringFormatter \"Test Debug\"\n", result)
	b.Reset()

	xlog.SetGlobalLogLevel(xlog.DEBUG)

	log2 := logger.WithValues("count", 1)
	log2.KV(xlog.DEBUG, "level", "debug")
	result = b.String()
	expected := "time=2021-04-01T00:00:00Z level=D pkg=xlog_test func=Test_StringFormatter count=1 level=\"debug\"\n"
	assert.Equal(t, expected, result)
	b.Reset()

	date, err := time.Parse("2006-01-02", "2021-04-01")
	require.NoError(t, err)

	log2.KV(xlog.INFO,
		"int", 1, // int
		"nint", -2, // negative int
		"uint64", uint64(123456789123456), // int
		"bool", false,
		"time", date, // time.Time
		"strings", []string{"s1", "s2"},
		"err", withAnnotateError("logs error", 2),
	)
	result = b.String()
	expected = "time=2021-04-01T00:00:00Z level=I pkg=xlog_test func=Test_StringFormatter count=1 int=1 nint=-2 uint64=123456789123456 bool=false time=\"2021-04-01T00:00:00Z\" strings=[\"s1\",\"s2\"] err=\"originateError: msg=logs error, level=0\\ngithub.com/effective-security/xlog_test.originateError\\n"
	assert.Contains(t, result, expected)
	b.Reset()
}

type someSvc struct{}

func (s *someSvc) log(msg string) {
	logger.Info(msg)
}

func TestXlogString(t *testing.T) {
	date, err := time.Parse("2006-01-02", "2021-04-01")
	require.NoError(t, err)

	structVal := struct {
		S string
		N int
		D time.Time
	}{
		"str", 1, date,
	}

	tcases := []struct {
		name string
		val  interface{}
		exp  string
	}{
		{"int", 1, "1"},
		{"nint", -72349568723, "-72349568723"},
		{"bool", false, "false"},
		{"strings", []string{"s1", "s2"}, `["s1","s2"]`},
		{"date", date, `"2021-04-01T00:00:00Z"`},
		{"struct", structVal, `{"S":"str","N":1,"D":"2021-04-01T00:00:00Z"}`},
	}

	for _, tc := range tcases {
		assert.Equal(t, tc.exp, xlog.EscapedString(tc.val), tc.name)
	}
}

func Test_ColorFormatterDebug(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	xlog.SetFormatter(xlog.NewPrettyFormatter(writer).Options(xlog.FormatNoCaller, xlog.FormatWithColor))
	xlog.SetGlobalLogLevel(xlog.DEBUG)

	logger.Infof("Test Info")
	expected := "2021-04-01 00:00:00.000000 \x1b[0;96mI | pkg=xlog_test, \"Test Info\"\x1b[0m\n"
	assert.Equal(t, expected, b.String())
	b.Reset()

	logger.KV(xlog.INFO, "k1", 1, "err", fmt.Errorf("not found"))
	writer.Flush()
	expected = "2021-04-01 00:00:00.000000 \x1b[0;96mI | pkg=xlog_test, k1=1, err=\"not found\"\x1b[0m\n"
	assert.Equal(t, expected, b.String())
	b.Reset()

	logger.Errorf("Test Error")
	expected = "2021-04-01 00:00:00.000000 \x1b[0;91mE | pkg=xlog_test, \"Test Error\"\x1b[0m\n"
	assert.Equal(t, expected, b.String())
	b.Reset()

	logger.Error("unable to find: ", fmt.Errorf("not found"))
	expected = "2021-04-01 00:00:00.000000 \x1b[0;91mE | pkg=xlog_test, \"unable to find: \", \"not found\"\x1b[0m\n"
	assert.Equal(t, expected, b.String())
	b.Reset()

	logger.Warningf("Test Warning")
	expected = "2021-04-01 00:00:00.000000 \x1b[0;93mW | pkg=xlog_test, \"Test Warning\"\x1b[0m\n"
	assert.Equal(t, expected, b.String())
	b.Reset()

	logger.Tracef("Test Trace")
	expected = "2021-04-01 00:00:00.000000 \x1b[0;37mT | pkg=xlog_test, \"Test Trace\"\x1b[0m\n"
	assert.Equal(t, expected, b.String())
	b.Reset()

	logger.Debugf("Test Debug")
	expected = "2021-04-01 00:00:00.000000 \x1b[0;35mD | pkg=xlog_test, \"Test Debug\"\x1b[0m\n"
	assert.Equal(t, expected, b.String())
	b.Reset()
}

func Test_WithCaller(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	xlog.SetFormatter(xlog.NewPrettyFormatter(writer).Options(xlog.FormatWithCaller, xlog.FormatWithColor))

	logger.Infof("Test Info")
	result := b.String()
	assert.Equal(t, "2021-04-01 00:00:00.000000 \x1b[0;96mI | pkg=xlog_test, func=Test_WithCaller, \"Test Info\"\x1b[0m\n", result)
	b.Reset()

	xlog.SetFormatter(xlog.NewStringFormatter(writer).Options(xlog.FormatWithCaller, xlog.FormatSkipTime))
	logger.Infof("Test Info")
	writer.Flush()
	result = b.String()
	assert.Equal(t, "level=I pkg=xlog_test func=Test_WithCaller \"Test Info\"\n", result)
	b.Reset()

}

func Test_NilFormatter(t *testing.T) {
	f := xlog.NewNilFormatter()
	f.FormatKV("pkg", xlog.DEBUG, 1)
	f.Format("pkg", xlog.DEBUG, 1)
	f.Flush()
}
