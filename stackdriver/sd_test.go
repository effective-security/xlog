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

func Test_Formatter(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	xlog.SetGlobalLogLevel(xlog.INFO)
	xlog.SetFormatter(NewFormatter(writer, "sd").WithCaller(true))

	logger.Info("Test Info")
	result := b.String()
	assert.Contains(t, result, "{\"logName\":\"sd\",\"component\":\"stackdriver\",\"timestamp\":")
	assert.Contains(t, result, "\"textPayload\":\"src=Test_Formatter, Test Info\",\"severity\":\"INFO\",\"sourceLocation\":{\"function\":\"Test_Formatter\"}}\n")
	b.Reset()

	k3 := struct {
		Foo string
	}{Foo: "bar"}

	logger.KV(xlog.INFO, "k1", 1, "k2", false, "k3", k3)
	result = b.String()
	assert.Contains(t, result, "\"textPayload\":\"src=Test_Formatter, k1=1, k2=false, k3={\\\"Foo\\\":\\\"bar\\\"}\",\"severity\":\"INFO\",\"sourceLocation\":{\"function\":\"Test_Formatter\"}}\n")
	b.Reset()

	logger.KV(xlog.ERROR, "err", fmt.Errorf("log error"))
	result = b.String()
	assert.Contains(t, result, "\"textPayload\":\"src=Test_Formatter, err=\\\"log error\\\"\",\"severity\":\"ERROR\",\"sourceLocation\":{\"file\":\"sd_test.go\",\"line\":37,\"function\":\"Test_Formatter\"}}\n")
	b.Reset()
}

func Test_FormatterFunc(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	xlog.SetGlobalLogLevel(xlog.INFO)
	xlog.SetFormatter(NewFormatter(writer, "sd").WithCaller(true))

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
