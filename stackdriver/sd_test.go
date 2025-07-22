package stackdriver

import (
	"bufio"
	"bytes"
	goerrors "errors"
	"testing"
	"time"

	"github.com/effective-security/xlog"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

var logger = xlog.NewPackageLogger("github.com/effective-security/xlog", "stackdriver")

func Test_FormatterOptions(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	xlog.SetGlobalLogLevel(xlog.INFO)
	xlog.SetFormatter(NewFormatter(writer, "sd").
		Options(xlog.FormatWithCaller, xlog.FormatWithLocation, xlog.FormatSkipTime, xlog.FormatPrintEmpty))

	logger.KV(xlog.INFO, "k1", 1, "k2", false, "nil", nil, "empty", "")
	result := b.String()
	assert.Equal(t, `{"logName":"sd","component":"stackdriver","message":{"k1":1,"k2":false,"nil":null,"empty":""},"severity":"INFO","sourceLocation":{"file":"sd_test.go","line":25,"function":"Test_FormatterOptions"}}`+"\n", result)
	b.Reset()

	xlog.SetFormatter(NewFormatter(writer, "sd").
		Options(xlog.FormatNoCaller, xlog.FormatWithLocation, xlog.FormatSkipTime, xlog.FormatPrintEmpty))

	logger.KV(xlog.INFO, "k1", 1, "k2", false, "nil", nil, "empty", "")
	result = b.String()
	assert.Equal(t, `{"logName":"sd","component":"stackdriver","message":{"k1":1,"k2":false,"nil":null,"empty":""},"severity":"INFO","sourceLocation":{"function":"Test_FormatterOptions"}}`+"\n", result)
	b.Reset()

	xlog.SetFormatter(NewFormatter(writer, "sd").
		Options(xlog.FormatNoCaller, xlog.FormatWithLocation, xlog.FormatSkipTime))

	stru := struct {
		A   string
		B   int
		C   float64
		D   uint64
		E   error
		Is  bool
		Dur time.Duration
	}{A: "a", B: 1, C: 1.1, D: 123, E: errors.New("error"), Dur: time.Second}

	logger.KV(xlog.INFO, "k1", 1, "k2", false, "nil", nil, "empty", "", "zero", 0, "struct", stru)
	result = b.String()
	assert.Equal(t, `{"logName":"sd","component":"stackdriver","message":{"k1":1,"k2":false,"zero":0,"struct":{"A":"a","B":1,"C":1.1,"D":123,"E":{},"Is":false,"Dur":1000000000}},"severity":"INFO","sourceLocation":{"function":"Test_FormatterOptions"}}`+"\n", result)
	b.Reset()

	assert.Panics(t, func() {
		logger.KV(xlog.INFO, 1, 2)
	})
	assert.Panics(t, func() {
		logger.KV(xlog.INFO, errors.New("not a string"))
	})
}

func Test_Formatter(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	xlog.SetGlobalLogLevel(xlog.INFO)
	xlog.SetFormatter(NewFormatter(writer, "sd").Options(xlog.FormatWithCaller))

	xlog.TimeNowFn = func() time.Time {
		return time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	defer func() {
		xlog.TimeNowFn = time.Now
	}()

	logger.Info("Test Info")
	result := b.String()
	assert.Equal(t, `{"logName":"sd","component":"stackdriver","timestamp":"2019-01-01T00:00:00Z","message":{"msg":"Test Info"},"severity":"INFO","sourceLocation":{"function":"Test_Formatter"}}`+"\n", result)
	b.Reset()

	k3 := struct {
		Foo string
	}{Foo: "bar"}

	logger.KV(xlog.INFO, "k1", 1, "k2", false, "k3", k3, "nil", nil, "empty", "")
	result = b.String()
	assert.Equal(t, `{"logName":"sd","component":"stackdriver","timestamp":"2019-01-01T00:00:00Z","message":{"k1":1,"k2":false,"k3":{"Foo":"bar"}},"severity":"INFO","sourceLocation":{"function":"Test_Formatter"}}`+"\n", result)
	b.Reset()

	logger.KV(xlog.ERROR, "err", goerrors.New("log error"))
	result = b.String()
	assert.Equal(t, `{"logName":"sd","component":"stackdriver","timestamp":"2019-01-01T00:00:00Z","message":{"err":"log error"},"severity":"ERROR","sourceLocation":{"file":"sd_test.go","line":92,"function":"Test_Formatter"}}`+"\n", result)
	b.Reset()
}

func Test_FormatterFunc(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	xlog.SetGlobalLogLevel(xlog.INFO)
	xlog.SetFormatter(NewFormatter(writer, "sd").Options(xlog.FormatWithCaller))

	xlog.TimeNowFn = func() time.Time {
		return time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	defer func() {
		xlog.TimeNowFn = time.Now
	}()

	func() {
		logger.Info("Test Info")
	}()
	result := b.String()
	assert.Equal(t, `{"logName":"sd","component":"stackdriver","timestamp":"2019-01-01T00:00:00Z","message":{"msg":"Test Info"},"severity":"INFO","sourceLocation":{"function":"Test_FormatterFunc.func3"}}`+"\n", result)
	b.Reset()

	func() {
		s := someSvc{}
		s.log("Test Info")
	}()
	result = b.String()
	assert.Equal(t, `{"logName":"sd","component":"stackdriver","timestamp":"2019-01-01T00:00:00Z","message":{"msg":"Test Info"},"severity":"INFO","sourceLocation":{"function":"log"}}`+"\n", result)
}

type someSvc struct{}

func (s *someSvc) log(msg string) {
	logger.Info(msg)
}
