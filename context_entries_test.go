package xlog_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/effective-security/xlog"
)

func TestContextEntries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	assert.Nil(t, xlog.ContextEntries(ctx))

	ctx1 := xlog.ContextWithKV(ctx, "a", 1)
	entries := xlog.ContextEntries(ctx1)
	require.Len(t, entries, 2)
	assert.Equal(t, []any{"a", 1}, entries)

	ctx2 := xlog.ContextWithKV(ctx1, "b", "two")
	entries2 := xlog.ContextEntries(ctx2)
	require.Len(t, entries2, 4)
	assert.Equal(t, []any{"a", 1, "b", "two"}, entries2)
}
