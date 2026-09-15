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
	"sync/atomic"
	"time"
)

// ChannelWriter copies writes into a bounded queue consumed by one goroutine.
// A full queue blocks producers. Destination write and flush errors are ignored.
// Quiesce all producers before Stop and never write afterward. See FINDINGS.md
// for shutdown limitations. A ChannelWriter must not be copied after first use.
type ChannelWriter struct {
	write    chan []byte
	stop     chan bool
	stopped  chan bool
	running  atomic.Bool
	buffPool sync.Pool
}

// NewChannelWriter starts a worker that writes to dest. bufferDepth is the
// number of queued byte slices, not a byte or memory limit; zero is unbuffered
// and a negative depth panics. The worker calls Flush() error on dest at positive
// flushInterval ticks and on Stop, when supported. Zero disables periodic flush.
// Queued writes may be lost on a crash. The destination is never closed.
func NewChannelWriter(dest io.Writer, bufferDepth int, flushInterval time.Duration) *ChannelWriter {
	cw := ChannelWriter{
		write:   make(chan []byte, bufferDepth),
		stop:    make(chan bool),
		stopped: make(chan bool),
	}
	cw.running.Store(true)
	cw.buffPool.New = func() any {
		return make([]byte, 0, 256)
	}
	go cw.listen(dest, flushInterval)
	return &cw
}

// IsStopped reports whether stopping has begun, not whether draining has finished.
func (cw *ChannelWriter) IsStopped() bool {
	return !cw.running.Load()
}

// Stop requests shutdown. The first caller waits for queued writes and the
// destination's Flush, if supported; subsequent callers return immediately.
// Stop can block indefinitely on a stalled destination. Stop producers first.
func (cw *ChannelWriter) Stop() {
	if cw.running.CompareAndSwap(true, false) {
		cw.stop <- true
		<-cw.stopped // wait til we've finished draining the queue and have flushed the output
	}
}

// Write implements the io.Writer interface
func (cw *ChannelWriter) Write(d []byte) (int, error) {
	// the documented sematics of Write are that we can't hold onto the supplied
	// bytes past the end of the function, so we need to create a copy to Put
	// on the channel.
	buff := cw.buffPool.Get().([]byte)
	buff = append(buff[:0], d...)
	cw.write <- buff
	return len(d), nil
}

type flushable interface {
	Flush() error
}

// listen is our background go-routine, it reads from the channel and does
// the writes. It also flushes on a regular basis if configured to do so.
func (cw *ChannelWriter) listen(dest io.Writer, flushInterval time.Duration) {
	defer func() {
		cw.stopped <- true
	}()
	var flushChan <-chan time.Time
	flusher, canFlush := dest.(flushable)
	if canFlush && flushInterval > 0 {
		ft := time.NewTicker(flushInterval)
		flushChan = ft.C
		defer ft.Stop()
	} else {
		flushChan = make(chan time.Time)
	}
	for {
		select {
		case <-flushChan:
			if canFlush {
				_ = flusher.Flush()
			}
		case b := <-cw.write:
			_, _ = dest.Write(b)
			cw.buffPool.Put(b) //nolint:staticcheck
		case <-cw.stop:
			// drain what's left of the Write channel
			for {
				select {
				case b := <-cw.write:
					_, _ = dest.Write(b)
					cw.buffPool.Put(b) //nolint:staticcheck
				default:
					if canFlush {
						_ = flusher.Flush()
					}
					return
				}
			}
		}
	}
}
