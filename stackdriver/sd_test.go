package stackdriver

import (
	"bufio"
	"bytes"
	"fmt"
	"testing"

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
	assert.Equal(t, "{\"logName\":\"sd\",\"component\":\"stackdriver\",\"textPayload\":\"src=Test_FormatterOptions, k1=1, k2=false, nil=null, empty=\\\"\\\"\",\"severity\":\"INFO\",\"sourceLocation\":{\"file\":\"sd_test.go\",\"line\":23,\"function\":\"Test_FormatterOptions\"}}\n", result)
	b.Reset()
}

func Test_Formatter(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	xlog.SetGlobalLogLevel(xlog.INFO)
	xlog.SetFormatter(NewFormatter(writer, "sd").Options(xlog.FormatWithCaller))

	logger.Info("Test Info")
	result := b.String()
	assert.Contains(t, result, "{\"logName\":\"sd\",\"component\":\"stackdriver\",\"timestamp\":")
	assert.Contains(t, result, "\"textPayload\":\"src=Test_Formatter, Test Info\",\"severity\":\"INFO\",\"sourceLocation\":{\"function\":\"Test_Formatter\"}}\n")
	b.Reset()

	k3 := struct {
		Foo string
	}{Foo: "bar"}

	logger.KV(xlog.INFO, "k1", 1, "k2", false, "k3", k3, "nil", nil, "empty", "")
	result = b.String()
	assert.Contains(t, result, "\"textPayload\":\"src=Test_Formatter, k1=1, k2=false, k3={\\\"Foo\\\":\\\"bar\\\"}\",\"severity\":\"INFO\",\"sourceLocation\":{\"function\":\"Test_Formatter\"}}\n")
	b.Reset()

	logger.KV(xlog.ERROR, "err", fmt.Errorf("log error"))
	result = b.String()
	assert.Contains(t, result, "\"textPayload\":\"src=Test_Formatter, err=\\\"log error\\\"\",\"severity\":\"ERROR\",\"sourceLocation\":{\"file\":\"sd_test.go\",\"line\":51,\"function\":\"Test_Formatter\"}}\n")
	b.Reset()
}

func Test_FormatterFunc(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	xlog.SetGlobalLogLevel(xlog.INFO)
	xlog.SetFormatter(NewFormatter(writer, "sd").Options(xlog.FormatWithCaller))

	func() {
		logger.Info("Test Info")
	}()
	result := b.String()
	assert.Contains(t, result, "{\"logName\":\"sd\",\"component\":\"stackdriver\",\"timestamp\":")
	assert.Contains(t, result, "\"textPayload\":\"src=Test_FormatterFunc.func1, Test Info\",\"severity\":\"INFO\",\"sourceLocation\":{\"function\":\"Test_FormatterFunc.func1\"}}\n")
	b.Reset()

	func() {
		s := someSvc{}
		s.log("Test Info")
	}()
	result = b.String()
	assert.Contains(t, result, "{\"logName\":\"sd\",\"component\":\"stackdriver\",\"timestamp\"")
	assert.Contains(t, result, "\"textPayload\":\"src=log, Test Info\",\"severity\":\"INFO\",\"sourceLocation\":{\"function\":\"log\"}}\n")
}

type someSvc struct{}

func (s *someSvc) log(msg string) {
	logger.Info(msg)
}
