package logrotate_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/xlog"
	"github.com/effective-security/xlog/logrotate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type extraSink struct {
	bytes.Buffer
	flushes  int
	closed   bool
	writeErr error
	flushErr error
}

func (s *extraSink) Write(b []byte) (int, error) {
	if s.writeErr != nil {
		return 0, s.writeErr
	}
	return s.Buffer.Write(b)
}
func (s *extraSink) Flush() error { s.flushes++; return s.flushErr }
func (s *extraSink) Close() error { s.closed = true; return nil }

func TestRotationAllModes(t *testing.T) {
	for _, buffered := range []bool{false, true} {
		for _, withExtra := range []bool{false, true} {
			t.Run(fmt.Sprintf("buffered=%v/extra=%v", buffered, withExtra), func(t *testing.T) {
				dir := t.TempDir()
				old := xlog.GetFormatter()
				extra := &extraSink{}
				var sink io.Writer
				if withExtra {
					sink = extra
				}
				closer, err := logrotate.Initialize(dir, "logs", 1, 1, buffered, sink)
				require.NoError(t, err)
				t.Cleanup(func() { assert.NoError(t, closer.Close()) })
				formatter := xlog.GetFormatter()
				p := xlog.NewPackageLogger(t.Name(), "rotation")
				const records = 400
				for i := range records {
					p.KV(xlog.INFO, "record", i)
				}
				var closers sync.WaitGroup
				for range 4 {
					closers.Go(func() { assert.NoError(t, closer.Close()) })
				}
				closers.Wait()
				require.Equal(t, old, xlog.GetFormatter())
				data, err := os.ReadFile(filepath.Join(dir, "logs.log"))
				require.NoError(t, err)
				require.Equal(t, records, bytes.Count(data, []byte("\n")))
				if withExtra {
					require.Equal(t, data, extra.Bytes())
					require.Positive(t, extra.flushes)
					require.False(t, extra.closed)
				}
				formatter.Format("late", xlog.INFO, 0, "after close")
				require.ErrorIs(t, formatter.(xlog.ErrorFormatter).Err(), io.ErrClosedPipe)
			})
		}
	}
}

func TestRotationOverlap(t *testing.T) {
	for _, outerFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("outer-first=%v", outerFirst), func(t *testing.T) {
			old := xlog.GetFormatter()
			dir := t.TempDir()
			outer, err := logrotate.Initialize(dir, "outer", 1, 1, true, nil)
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, outer.Close()) })
			outerFormatter := xlog.GetFormatter()
			inner, err := logrotate.Initialize(dir, "inner", 1, 1, true, nil)
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, inner.Close()) })
			innerFormatter := xlog.GetFormatter()
			if outerFirst {
				require.NoError(t, outer.Close())
				require.Same(t, innerFormatter, xlog.GetFormatter())
				require.NoError(t, inner.Close())
			} else {
				require.NoError(t, inner.Close())
				require.Same(t, outerFormatter, xlog.GetFormatter())
				require.NoError(t, outer.Close())
			}
			require.Equal(t, old, xlog.GetFormatter())
		})
	}
}

func TestRotationErrors(t *testing.T) {
	for _, buffered := range []bool{false, true} {
		t.Run(fmt.Sprintf("buffered=%v", buffered), func(t *testing.T) {
			for _, operation := range []string{"write", "flush", "file-open"} {
				t.Run(operation, func(t *testing.T) {
					dir := t.TempDir()
					sentinel := errors.New("sink failed")
					extra := &extraSink{}
					var sink io.Writer = extra
					baseFilename := "logs"
					switch operation {
					case "write":
						extra.writeErr = sentinel
					case "flush":
						extra.flushErr = sentinel
					case "file-open":
						require.NoError(t, os.WriteFile(filepath.Join(dir, "blocked"), nil, 0600))
						baseFilename = "blocked/logs"
						sink = nil
					}
					closer, err := logrotate.Initialize(dir, baseFilename, 1, 1, buffered, sink)
					require.NoError(t, err)
					xlog.NewPackageLogger(t.Name(), "failure").Info("message")
					err = closer.Close()
					if operation == "file-open" {
						// Lumberjack formats its filesystem cause into an error string.
						require.ErrorContains(t, err, "blocked")
					} else {
						require.ErrorIs(t, err, sentinel)
					}
					require.Same(t, err, closer.Close())
					require.False(t, extra.closed)
				})
			}
		})
	}
}

func TestRotationFlushAndFatalDelivery(t *testing.T) {
	for _, buffered := range []bool{false, true} {
		t.Run(fmt.Sprintf("buffered=%v", buffered), func(t *testing.T) {
			dir := t.TempDir()
			closer, err := logrotate.Initialize(dir, "logs", 1, 1, buffered, nil)
			require.NoError(t, err)
			defer func() { assert.NoError(t, closer.Close()) }()
			p := xlog.NewPackageLogger(t.Name(), "flush")
			p.Info("visible after flush")
			require.NoError(t, p.FlushError())
			data, err := os.ReadFile(filepath.Join(dir, "logs.log"))
			require.NoError(t, err)
			require.Contains(t, string(data), "visible after flush")
			oldExit := xlog.ExitFunc
			defer func() { xlog.ExitFunc = oldExit }()
			called := false
			xlog.ExitFunc = func(code int) {
				called = true
				assert.Equal(t, 1, code)
				data, err := os.ReadFile(filepath.Join(dir, "logs.log"))
				require.NoError(t, err)
				require.Contains(t, string(data), "fatal delivered")
			}
			p.Fatalf("fatal %s", "delivered")
			require.True(t, called)
		})
	}
}

func TestChannelWriterBarrierAndErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dest := &shutdownWriter{
			releaseWrite: make(chan struct{}),
			releaseFlush: make(chan struct{}),
			flushStarted: make(chan struct{}),
		}
		cw := logrotate.NewChannelWriter(dest, 1, 0)
		_, err := cw.Write([]byte("before barrier"))
		require.NoError(t, err)
		done := make(chan error, 1)
		go func() { done <- cw.Flush() }()
		synctest.Wait()
		require.Empty(t, done)
		close(dest.releaseWrite)
		<-dest.flushStarted
		synctest.Wait()
		require.Empty(t, done)
		close(dest.releaseFlush)
		require.NoError(t, <-done)
		require.Equal(t, "before barrier", dest.String())
		// This fixture signals each flush by closing the channel; give the final
		// flush a fresh channel after the barrier completed.
		dest.flushStarted = make(chan struct{})
		require.NoError(t, cw.Close())
		require.NoError(t, cw.Flush())
	})
	for _, operation := range []string{"write", "flush"} {
		sentinel := errors.New(operation)
		dest := &extraSink{}
		if operation == "write" {
			dest.writeErr = sentinel
		} else {
			dest.flushErr = sentinel
		}
		cw := logrotate.NewChannelWriter(dest, 1, 0)
		_, err := cw.Write([]byte("data"))
		require.NoError(t, err)
		require.ErrorIs(t, cw.Flush(), sentinel)
		require.ErrorIs(t, cw.Err(), sentinel)
		require.ErrorIs(t, cw.Close(), sentinel)
		require.ErrorIs(t, cw.Flush(), sentinel)
	}
}

func TestChannelWriterFlushDuringStop(t *testing.T) {
	for _, depth := range []int{0, 1} {
		t.Run(fmt.Sprintf("depth=%d", depth), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				dest := &shutdownWriter{
					releaseWrite: make(chan struct{}),
					releaseFlush: make(chan struct{}),
					flushStarted: make(chan struct{}),
				}
				cw := logrotate.NewChannelWriter(dest, depth, 0)
				_, err := cw.Write([]byte("first"))
				require.NoError(t, err)
				synctest.Wait()
				if depth > 0 {
					_, err = cw.Write([]byte("queued"))
					require.NoError(t, err)
				}
				flushed := make(chan error, 2)
				go func() { flushed <- cw.Flush() }()
				synctest.Wait()
				require.Empty(t, flushed)
				stopped := make(chan struct{})
				go func() { cw.Stop(); close(stopped) }()
				synctest.Wait()
				require.True(t, cw.IsStopped())
				go func() { flushed <- cw.Flush() }()
				synctest.Wait()
				require.Empty(t, flushed, "both flush calls must wait for shutdown")
				close(dest.releaseWrite)
				<-dest.flushStarted
				synctest.Wait()
				require.Empty(t, flushed, "shutdown must finish the destination flush")
				close(dest.releaseFlush)
				require.NoError(t, <-flushed)
				require.NoError(t, <-flushed)
				<-stopped
			})
		})
	}
}
