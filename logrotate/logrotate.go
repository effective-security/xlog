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
	"bufio"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/xlog"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

const (
	fileBufferSize   = 8 << 10
	queueDepth       = 256
	logDirectoryMode = 0755
)

type logrotator struct {
	removeFormatter func()
	formatter       xlog.Formatter
	sink            *rotationSink
	channel         *ChannelWriter
	closeOnce       sync.Once
	closeErr        error
}

// Initialize creates a lumberjack log rotator and redirects log output to it.
// maxAge is retention in days; maxSize is maximum file size in lumberjack MB.
// The output is logFolder/baseFilename.log. File opening is lazy; later failures
// are reported by the returned closer. buffered enables a 256-item byte queue
// with a one-second flush interval. Without extraSink, an 8 KiB file buffer is
// used even when buffered is false; explicit logger flushing makes it visible.
// An extra sink is flushed when supported, but is never closed by this package.
// Close removes this formatter override, waits for admitted logging calls,
// drains, flushes, and closes the owned file. Overrides may close in any order.
// Concurrent and repeated Close calls wait and return the same result.
func Initialize(logFolder, baseFilename string, maxAge, maxSize int, buffered bool, extraSink io.Writer) (io.Closer, error) {
	if err := os.MkdirAll(logFolder, logDirectoryMode); err != nil {
		return nil, errors.Wrapf(err, "unable to create log directory %s", logFolder)
	}
	fileWriter := &lumberjack.Logger{
		Filename: filepath.Join(logFolder, baseFilename+".log"),
		MaxAge:   maxAge,
		MaxSize:  maxSize,
	}
	sink := &rotationSink{file: fileWriter}
	if extraSink != nil {
		sink.writer = io.MultiWriter(fileWriter, extraSink)
		if flusher, ok := extraSink.(flushable); ok {
			sink.flushers = append(sink.flushers, flusher)
		}
	} else {
		fileBuffer := bufio.NewWriterSize(fileWriter, fileBufferSize)
		sink.writer = fileBuffer
		sink.flushers = append(sink.flushers, fileBuffer)
	}
	l := &logrotator{sink: sink}
	var destination io.Writer = sink
	if buffered {
		l.channel = NewChannelWriter(sink, queueDepth, time.Second)
		destination = l.channel
	}
	l.formatter = xlog.NewDefaultFormatter(destination)
	l.removeFormatter = xlog.InstallFormatter(l.formatter)
	return l, nil
}

// Close removes this override and waits for draining, flushing, and file closure.
// It preserves delivery and close error causes. It does not guarantee fsync.
func (c *logrotator) Close() error {
	c.closeOnce.Do(func() {
		c.removeFormatter()
		flushErr := xlog.FlushFormatter(c.formatter)
		var channelErr error
		if c.channel != nil {
			channelErr = c.channel.Close()
		}
		c.closeErr = errors.Join(flushErr, channelErr, c.sink.Close())
	})
	return c.closeErr
}

// rotationSink owns the file. Its lock also rejects late direct formatter calls
// after Close, preventing lumberjack from reopening a file after shutdown.
type rotationSink struct {
	mu       sync.Mutex
	writer   io.Writer
	file     io.Closer
	flushers []flushable
	closed   bool
	err      error
}

func (s *rotationSink) record(err error) {
	if s.err == nil {
		s.err = err
	}
}

func (s *rotationSink) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, errors.WithMessage(io.ErrClosedPipe, "unable to write closed log destination")
	}
	n, err := s.writer.Write(b)
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	err = errors.WithMessage(err, "unable to write rotating log")
	s.record(err)
	return n, err
}

func (s *rotationSink) flush() {
	for _, flusher := range s.flushers {
		s.record(errors.WithMessage(flusher.Flush(), "unable to flush rotating log"))
	}
}

func (s *rotationSink) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.flush()
	}
	return s.err
}

func (s *rotationSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		s.flush()
		s.err = errors.Join(s.err, errors.WithMessage(s.file.Close(), "unable to close rotating log file"))
	}
	return s.err
}
