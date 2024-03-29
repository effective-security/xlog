package xlog_test

import (
	"bytes"
	"log"
	"testing"

	"github.com/effective-security/xlog"
	"github.com/stretchr/testify/assert"
)

func Test_Hijack(t *testing.T) {
	w := bytes.NewBuffer([]byte{})
	f := xlog.NewStringFormatter(w)
	xlog.SetFormatter(f)
	xlog.SetGlobalLogLevel(xlog.INFO)

	log.SetFlags(0)
	log.SetPrefix("prefix:")
	log.Println("testing")

	assert.Equal(t, "time=2021-04-01T00:00:00Z level=I func=Test_Hijack log=\"prefix:testing\"\n", w.String())
}

func Test_Hijack_Error(t *testing.T) {
	w := bytes.NewBuffer([]byte{})
	f := xlog.NewStringFormatter(w)
	xlog.SetFormatter(f)
	xlog.SetGlobalLogLevel(xlog.ERROR)

	log.Println("testing")

	assert.Empty(t, w.Bytes())
}
