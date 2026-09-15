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
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/xlog"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

type logrotator struct {
	oldFormatter xlog.Formatter
	logger       io.Writer
	fileBuf      *bufio.Writer // non-nil only when extraSink is nil; flushed on Close
	channel      *ChannelWriter
	closed       bool
}

// Initialize creates a lumberjack log rotator and redirects logs output to it.
// maxAge is retention in days; maxSize is maximum file size in lumberjack MB.
// The output is logFolder/baseFilename.log. Directory creation is checked, but
// file opening is lazy, so success does not prove that later writes will succeed.
// buffered enables a 256-item byte queue with a one-second flush interval.
// Without extraSink, an 8 KiB file buffer is used even when buffered is false.
// With extraSink, io.MultiWriter writes to the file and then extraSink; a file
// error prevents that write from reaching extraSink, which is not flushed here.
// Stop logging before calling Close on the returned io.Closer. Close restores
// the prior formatter, but has known drain/error/resource issues (FINDINGS.md).
func Initialize(logFolder, baseFilename string, maxAge, maxSize int, buffered bool, extraSink io.Writer) (io.Closer, error) {
	err := os.MkdirAll(logFolder, 0755)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	fileWriter := lumberjack.Logger{
		Filename: filepath.Join(logFolder, baseFilename+".log"),
		MaxAge:   maxAge,
		MaxSize:  maxSize,
	}

	l := &logrotator{
		oldFormatter: xlog.GetFormatter(),
	}

	if extraSink != nil {
		// No bufio: every write goes to both file and extraSink immediately
		l.logger = io.MultiWriter(&fileWriter, extraSink)
	} else {
		fileBuf := bufio.NewWriterSize(&fileWriter, 8192)
		l.logger = fileBuf
		l.fileBuf = fileBuf
	}

	if buffered {
		l.channel = NewChannelWriter(l.logger, 256, time.Second)
	}

	xlog.SetFormatter(xlog.NewDefaultFormatter(l.destination()))

	return l, nil
}

func (c *logrotator) destination() io.Writer {
	if c.channel != nil {
		return c.channel
	}
	return c.logger
}

// Close attempts to flush buffers, restore the old formatter, and stop the
// worker. It returns an error on a repeated call. Calls must be serialized;
// destination errors and durable storage are not guaranteed by this API.
func (c *logrotator) Close() error {
	if c.closed {
		return errors.New("already closed")
	}
	c.closed = true

	// This buffer exists only without extraSink. With a worker, this flush can
	// race with writes; the required shutdown ordering is tracked in FINDINGS.md.
	if c.fileBuf != nil {
		_ = c.fileBuf.Flush()
	}

	// restore output
	xlog.SetFormatter(c.oldFormatter)

	if c.channel != nil {
		c.channel.Stop()
		c.channel = nil
	}
	return nil
}
