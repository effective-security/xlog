package logrotate_test

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/effective-security/xlog"
	"github.com/effective-security/xlog/logrotate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Rotate(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	tmpDir := t.TempDir()

	logRotate, err := logrotate.Initialize(tmpDir, "rotator", 1, 1, false, writer)
	require.NoError(t, err)
	defer func() {
		_ = logRotate.Close()
	}()

	logger := xlog.NewPackageLogger("github.com/effective-security/xlog", "logrotate")
	xlog.SetRepoLogLevel("github.com/effective-security/xlog", xlog.TRACE)

	logger.Debug("1")
	logger.Debugf("%d", 2)
	logger.Info("1")
	logger.Infof("%d", 2)
	logger.KV(xlog.INFO, "k", 2)
	logger.Error("1")
	logger.Errorf("%d", 2)
	logger.Trace("1")
	logger.Tracef("%d", 2)
	logger.Notice("1")
	logger.Noticef("%d", 2)

	_ = writer.Flush()
	assert.NotEmpty(t, b.Bytes())
}
