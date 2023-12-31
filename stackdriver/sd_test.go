package stackdriver

import (
	"bufio"
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/effective-security/xlog"
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
	assert.Equal(t, `{"logName":"sd","component":"stackdriver","jsonPayload":{"empty":"","k1":1,"k2":false,"nil":null},"severity":"INFO","sourceLocation":{"file":"sd_test.go","line":24,"function":"Test_FormatterOptions"}}`+"\n", result)
	b.Reset()
}

func Test_Formatter(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	xlog.SetGlobalLogLevel(xlog.INFO)
	xlog.SetFormatter(NewFormatter(writer, "sd").Options(xlog.FormatWithCaller))

	TimeNowFn = func() time.Time {
		return time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	defer func() {
		TimeNowFn = time.Now
	}()

	logger.Info("Test Info")
	result := b.String()
	assert.Equal(t, `{"logName":"sd","component":"stackdriver","timestamp":"2019-01-01T00:00:00Z","jsonPayload":{"msg":"Test Info"},"severity":"INFO","sourceLocation":{"function":"Test_Formatter"}}`+"\n", result)
	b.Reset()

	k3 := struct {
		Foo string
	}{Foo: "bar"}

	logger.KV(xlog.INFO, "k1", 1, "k2", false, "k3", k3, "nil", nil, "empty", "")
	result = b.String()
	assert.Equal(t, `{"logName":"sd","component":"stackdriver","timestamp":"2019-01-01T00:00:00Z","jsonPayload":{"empty":"","k1":1,"k2":false,"k3":{"Foo":"bar"}},"severity":"INFO","sourceLocation":{"function":"Test_Formatter"}}`+"\n", result)
	b.Reset()

	logger.KV(xlog.ERROR, "err", fmt.Errorf("log error"))
	result = b.String()
	assert.Equal(t, `{"logName":"sd","component":"stackdriver","timestamp":"2019-01-01T00:00:00Z","jsonPayload":{"err":{}},"severity":"ERROR","sourceLocation":{"file":"sd_test.go","line":58,"function":"Test_Formatter"}}`+"\n", result)
	b.Reset()
}

func Test_FormatterFunc(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	xlog.SetGlobalLogLevel(xlog.INFO)
	xlog.SetFormatter(NewFormatter(writer, "sd").Options(xlog.FormatWithCaller))

	TimeNowFn = func() time.Time {
		return time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	defer func() {
		TimeNowFn = time.Now
	}()

	func() {
		logger.Info("Test Info")
	}()
	result := b.String()
	assert.Equal(t, `{"logName":"sd","component":"stackdriver","timestamp":"2019-01-01T00:00:00Z","jsonPayload":{"msg":"Test Info"},"severity":"INFO","sourceLocation":{"function":"Test_FormatterFunc.func3"}}`+"\n", result)
	b.Reset()

	func() {
		s := someSvc{}
		s.log("Test Info")
	}()
	result = b.String()
	assert.Equal(t, `{"logName":"sd","component":"stackdriver","timestamp":"2019-01-01T00:00:00Z","jsonPayload":{"msg":"Test Info"},"severity":"INFO","sourceLocation":{"function":"log"}}`+"\n", result)
}

type someSvc struct{}

func (s *someSvc) log(msg string) {
	logger.Info(msg)
}
