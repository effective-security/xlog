package logrotate_test

import (
	"bytes"
	"fmt"
	"io"
	"testing"
	"testing/synctest"

	"github.com/effective-security/xlog/logrotate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type shutdownWriter struct {
	bytes.Buffer
	releaseWrite chan struct{}
	releaseFlush chan struct{}
	flushStarted chan struct{}
}

func (w *shutdownWriter) Write(b []byte) (int, error) {
	<-w.releaseWrite
	return w.Buffer.Write(b)
}

func (w *shutdownWriter) Flush() error {
	close(w.flushStarted)
	<-w.releaseFlush
	return nil
}

func TestChannelWriter_Shutdown(t *testing.T) {
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
				expected := "first"
				if depth > 0 {
					_, err = cw.Write([]byte("queued"))
					require.NoError(t, err)
					expected += "queued"
				}

				pendingDone := make(chan struct{})
				go func() {
					n, writeErr := cw.Write([]byte("blocked"))
					assert.Zero(t, n)
					assert.ErrorIs(t, writeErr, io.ErrClosedPipe)
					close(pendingDone)
				}()
				synctest.Wait()
				select {
				case <-pendingDone:
					t.Fatal("write did not block on the full queue")
				default:
				}

				const stopCallers = 3
				stopped := make(chan struct{}, stopCallers)
				for range stopCallers {
					go func() {
						cw.Stop()
						stopped <- struct{}{}
					}()
				}
				synctest.Wait()
				require.True(t, cw.IsStopped())
				require.Len(t, stopped, 0, "Stop returned before writes drained")
				select {
				case <-pendingDone:
				default:
					t.Fatal("shutdown did not release the blocked writer")
				}
				n, err := cw.Write([]byte("during stop"))
				require.Zero(t, n)
				require.ErrorIs(t, err, io.ErrClosedPipe)

				close(dest.releaseWrite)
				<-dest.flushStarted
				synctest.Wait()
				require.Len(t, stopped, 0, "Stop returned before final flush completed")
				close(dest.releaseFlush)
				for range stopCallers {
					<-stopped
				}
				require.Equal(t, expected, dest.String())
				cw.Stop()
				for _, data := range [][]byte{[]byte("after stop"), nil} {
					n, err = cw.Write(data)
					require.Zero(t, n)
					require.ErrorIs(t, err, io.ErrClosedPipe)
				}
			})
		})
	}
}

func TestChannelWriter_ConcurrentWriteStop(t *testing.T) {
	for _, depth := range []int{0, 1} {
		t.Run(fmt.Sprintf("depth=%d", depth), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const producers = 32
				var dest bytes.Buffer
				cw := logrotate.NewChannelWriter(&dest, depth, 0)
				start := make(chan struct{})
				accepted := make(chan int, producers)
				for range producers {
					go func() {
						<-start
						n, err := cw.Write([]byte("x"))
						if err != nil {
							assert.ErrorIs(t, err, io.ErrClosedPipe)
							assert.Zero(t, n)
						} else {
							assert.Equal(t, 1, n)
						}
						accepted <- n
					}()
				}
				go func() {
					<-start
					cw.Stop()
				}()
				close(start)
				synctest.Wait()
				cw.Stop()
				total := 0
				for range producers {
					total += <-accepted
				}
				require.Equal(t, total, dest.Len(), "every accepted byte must drain")
			})
		})
	}
}
