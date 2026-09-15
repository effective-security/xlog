package logrotate

// Copyright 2018 salesforce.com
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import (
	"io"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
)

// ChannelWriter copies writes into a bounded queue consumed by one goroutine.
// A full queue blocks producers. Err, Flush, and Close report destination errors.
// Stop rejects new writes and waits for accepted writes and the final flush.
// A ChannelWriter must not be copied after first use.
type ChannelWriter struct {
	write     chan writeRequest
	stop      chan struct{}
	stopped   chan struct{}
	admission sync.RWMutex // protects queue closure against active senders
	stopOnce  sync.Once
	buffPool  sync.Pool
	errMu     sync.Mutex
	err       error
}

type writeRequest struct {
	data    []byte
	flushed chan error
}

// NewChannelWriter starts a worker that writes to dest. bufferDepth is the
// number of queued byte slices, not a byte or memory limit; zero is unbuffered
// and a negative depth panics. The worker calls Flush() error on dest at positive
// flushInterval ticks and on Stop, when supported. Zero disables periodic flush.
// Queued writes may be lost on a crash. The destination is never closed.
func NewChannelWriter(dest io.Writer, bufferDepth int, flushInterval time.Duration) *ChannelWriter {
	cw := ChannelWriter{
		write:   make(chan writeRequest, bufferDepth),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	cw.buffPool.New = func() any {
		return make([]byte, 0, 256)
	}
	go cw.listen(dest, flushInterval)
	return &cw
}

// IsStopped reports whether stopping has begun, not whether draining has finished.
func (cw *ChannelWriter) IsStopped() bool {
	select {
	case <-cw.stop:
		return true
	default:
		return false
	}
}

// Stop rejects new writes, drains accepted writes, and flushes the destination
// if supported. Every caller waits for completion, including concurrent callers.
// Stop can block indefinitely on a stalled destination.
func (cw *ChannelWriter) Stop() {
	cw.stopOnce.Do(func() {
		// Release blocked senders before waiting for exclusive queue access.
		close(cw.stop)
		cw.admission.Lock()
		close(cw.write)
		cw.admission.Unlock()
	})
	<-cw.stopped
}

// Close stops the worker and returns the first destination error. It is safe to
// call concurrently or repeatedly. It does not close the caller-owned destination.
func (cw *ChannelWriter) Close() error {
	cw.Stop()
	return cw.Err()
}

// Err returns the first destination write, short-write, or flush error observed
// so far. It is safe during logging; enqueue success does not imply Err is nil.
func (cw *ChannelWriter) Err() error {
	cw.errMu.Lock()
	defer cw.errMu.Unlock()
	return cw.err
}

func (cw *ChannelWriter) recordError(err error) {
	if err == nil {
		return
	}
	cw.errMu.Lock()
	defer cw.errMu.Unlock()
	if cw.err == nil {
		cw.err = err
	}
}

// Flush waits for previously enqueued writes and flushes the destination when
// supported. During or after Stop it waits for shutdown and returns its error.
// It is a delivery barrier, not an fsync, and can block on a stalled destination.
func (cw *ChannelWriter) Flush() error {
	cw.admission.RLock()
	if cw.IsStopped() {
		cw.admission.RUnlock()
		<-cw.stopped
		return cw.Err()
	}
	flushed := make(chan error, 1)
	select {
	case cw.write <- writeRequest{flushed: flushed}:
		cw.admission.RUnlock()
		return <-flushed
	case <-cw.stop:
		cw.admission.RUnlock()
		<-cw.stopped
		return cw.Err()
	}
}

// Write copies d into the queue and reports enqueue success, not delivery.
// Writes after shutdown begins return zero and an error wrapping io.ErrClosedPipe.
// Writes overlapping shutdown may enqueue successfully or return that error;
// every successful write is drained before Stop returns.
func (cw *ChannelWriter) Write(d []byte) (int, error) {
	cw.admission.RLock()
	defer cw.admission.RUnlock()
	if cw.IsStopped() {
		return 0, errors.WithMessage(io.ErrClosedPipe, "unable to enqueue log write")
	}

	// The caller retains ownership of d after Write returns.
	buff := cw.buffPool.Get().([]byte)
	buff = append(buff[:0], d...)
	select {
	case cw.write <- writeRequest{data: buff}:
		return len(d), nil
	case <-cw.stop:
		cw.buffPool.Put(buff) //nolint:staticcheck
		return 0, errors.WithMessage(io.ErrClosedPipe, "unable to enqueue log write")
	}
}

type flushable interface {
	Flush() error
}

// listen is our background go-routine, it reads from the channel and does
// the writes. It also flushes on a regular basis if configured to do so.
func (cw *ChannelWriter) listen(dest io.Writer, flushInterval time.Duration) {
	defer close(cw.stopped)
	var flushChan <-chan time.Time
	flusher, canFlush := dest.(flushable)
	flush := func() {
		if canFlush {
			cw.recordError(errors.WithMessage(flusher.Flush(), "unable to flush queued log destination"))
		}
	}
	if canFlush && flushInterval > 0 {
		ft := time.NewTicker(flushInterval)
		flushChan = ft.C
		defer ft.Stop()
	}
	for {
		select {
		case <-flushChan:
			flush()
		case request, ok := <-cw.write:
			if !ok {
				flush()
				return
			}
			if request.flushed != nil {
				flush()
				request.flushed <- cw.Err()
				continue
			}
			b := request.data
			n, err := dest.Write(b)
			if err == nil && n != len(b) {
				err = io.ErrShortWrite
			}
			cw.recordError(errors.WithMessage(err, "unable to write queued log bytes"))
			cw.buffPool.Put(b) //nolint:staticcheck
		}
	}
}
