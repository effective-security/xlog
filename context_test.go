package xlog_test

import (
	"bufio"
	"bytes"
	"context"
	"testing"

	"github.com/effective-security/xlog"
	"github.com/stretchr/testify/assert"
)

func Test_ContextWithLog(t *testing.T) {
	ctx := xlog.ContextWithKV(context.Background(),
		"cid", 123,
		"uid", 234,
		"val", "original",
	)

	ctx2 := xlog.ContextWithKV(ctx, "val", "second")

	assert.Equal(t, ctx, ctx2)

	vals := xlog.ContextEntries(ctx)
	assert.Len(t, vals, 8)
}

func Test_WithContext(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	xlog.SetFormatter(xlog.NewPrettyFormatter(writer).Options(xlog.FormatWithCaller, xlog.FormatWithColor))

	ctx := xlog.ContextWithKV(context.Background(), "key1", 1, "key2", "val2")

	logger.ContextKV(ctx, xlog.INFO, "k3", 3)
	result := b.String()
	assert.Equal(t, "2021-04-01 00:00:00.000000 \x1b[0;96mI | pkg=xlog_test, func=Test_WithContext, key1=1, key2=\"val2\", k3=3\x1b[0m\n", result)
	b.Reset()
}
