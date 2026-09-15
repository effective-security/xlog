package logrotate

import (
	"io"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"
)

type ownedFile struct {
	closed   int
	closeErr error
}

func (f *ownedFile) Close() error { f.closed++; return f.closeErr }

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) { return 0, nil }

func TestRotationSinkOwnsFile(t *testing.T) {
	closeErr := errors.New("close failed")
	file := &ownedFile{closeErr: closeErr}
	sink := &rotationSink{
		writer: shortWriter{},
		file:   file,
	}
	n, err := sink.Write([]byte("short"))
	require.Zero(t, n)
	require.ErrorIs(t, err, io.ErrShortWrite)
	err = sink.Close()
	require.ErrorIs(t, err, io.ErrShortWrite)
	require.ErrorIs(t, err, closeErr)
	require.Equal(t, 1, file.closed)
	require.Same(t, err, sink.Close())
	require.Equal(t, 1, file.closed)
	n, err = sink.Write([]byte("closed"))
	require.Zero(t, n)
	require.ErrorIs(t, err, io.ErrClosedPipe)
}

func TestChannelWriterShortWrites(t *testing.T) {
	cw := NewChannelWriter(shortWriter{}, 1, 0)
	_, err := cw.Write([]byte("short"))
	require.NoError(t, err)
	require.ErrorIs(t, cw.Close(), io.ErrShortWrite)
}
