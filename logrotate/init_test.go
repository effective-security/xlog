package logrotate

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInitializeAndClose verifies that Initialize creates the log folder,
// returns a Closer that closes only once, and errors on subsequent Close.
func TestInitializeAndClose(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	closer, err := Initialize(dir, "testfile", 7, 5, false, nil)
	require.NoError(t, err)

	// The directory should exist
	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.True(t, info.IsDir(), "expected temp dir to be a directory")

	// Calling Close first time succeeds
	err = closer.Close()
	require.NoError(t, err)

	// Calling Close second time returns error
	err = closer.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already closed")

}
