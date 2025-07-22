package logrotate

import (
	"bufio"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testWriter struct {
	lock   sync.Mutex
	writes [][]byte
}

func (t *testWriter) Write(b []byte) (int, error) {
	c := make([]byte, len(b))
	copy(c, b)
	t.lock.Lock()
	defer t.lock.Unlock()
	t.writes = append(t.writes, c)
	return len(b), nil
}

func (t *testWriter) NumWrites() int {
	t.lock.Lock()
	defer t.lock.Unlock()
	return len(t.writes)
}

type testFlushWriter struct {
	testWriter
	flushCount int32
}

func (tf *testFlushWriter) Flush() error {
	tf.lock.Lock()
	defer tf.lock.Unlock()
	tf.flushCount++
	return nil
}

func (tf *testFlushWriter) NumFlushes() int32 {
	tf.lock.Lock()
	defer tf.lock.Unlock()
	return tf.flushCount
}

func TestChannelWriter_Flushes(t *testing.T) {
	dest := &testFlushWriter{}
	cw := NewChannelWriter(dest, 200, time.Millisecond)
	defer cw.Stop()
	waitUntil := time.Now().Add(time.Second)
	for dest.NumFlushes() == 0 {
		if time.Now().After(waitUntil) {
			require.FailNow(t, "Gave up waiting to be flushed")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestChannelWriter_BufioIsFlushable(t *testing.T) {
	dest := &testWriter{}
	w := bufio.NewWriter(dest)
	require.Implements(t, (*flushable)(nil), w, "bufio.Writer should be a flushable")
}

func TestChannelWriter_Writes(t *testing.T) {
	dest := &testWriter{}
	cw := NewChannelWriter(dest, 200, time.Millisecond)
	var writer io.Writer = cw // ensure cw can be used as an io.Writer

	defer cw.Stop()
	numMessages := 400
	exp := make([][]byte, 0, numMessages)
	for i := 0; i < numMessages; i++ {
		w := []byte(fmt.Sprintf("message %d", i))
		wcopy := append([]byte(nil), w...)
		exp = append(exp, wcopy)
		_, _ = writer.Write(w)
		// ensure that the writer doesn't hold onto the bytes the general
		// expectation for io.Write is that the caller owns the data after
		// write returns
		w[0] = 'X'
	}
	require.False(t, cw.IsStopped(), "ChannelWriter.IsStopped() reports true, but we haven't called Stop() yet")
	waitUntil := time.Now().Add(time.Second)
	for dest.NumWrites() < numMessages {
		if time.Now().After(waitUntil) {
			require.FailNowf(t,
				"Gave up waiting for background writes to turn up",
				"got %d out of %d", dest.NumWrites(), numMessages)
		}
		if dest.NumWrites() > 0 {
			// Stop should drain the channel, so we should still get all our expected messages
			cw.Stop()
			require.True(t, cw.IsStopped(), "Called Stop() on the ChannelWriter, but IsStopped() says no!")
		}
		time.Sleep(time.Millisecond)
	}
	dest.lock.Lock()
	defer dest.lock.Unlock()
	for i, e := range exp {
		assert.Equalf(t, e, dest.writes[i], "Write %d", i)
	}
}
