package xlog_test

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/xlog"
	"github.com/effective-security/xlog/stackdriver"
	"github.com/stretchr/testify/require"
	"gopkg.in/natefinch/lumberjack.v2"
)

func ExampleNewSink() {
	sink, err := xlog.NewSink(256)
	if err != nil {
		panic(err)
	}
	formatter := xlog.NewStringFormatter(os.Stdout,
		xlog.FormatSkipTime(true), xlog.FormatWithCaller(false), xlog.WithSink(sink))
	remove := xlog.InstallFormatter(formatter)
	l := xlog.NewPackageLogger("example.com/app", "main")
	l.KV(xlog.INFO, "event", "started")
	remove() // Wait for admitted producers before stopping the worker.
	if err := sink.Close(); err != nil {
		panic(err)
	}
	// Output: level=I pkg=main event=started
}

// application-example:start
// LogConfig selects the destination, format, and optional delivery worker.
type LogConfig struct {
	LogStd         bool   `help:"also output file logs to stderr"`
	LogDebug       bool   `help:"include source filename and line"`
	LogPretty      bool   `help:"use readable text"`
	LogJSON        bool   `help:"use JSON"`
	LogStackdriver bool   `help:"use Cloud Logging JSON"`
	LogDir         string `help:"log directory; empty uses stderr; /dev/null disables output"`
	Async          bool   `help:"deliver logs on a background worker"`
	BufSize        int    `help:"pending records in the sink; must be positive when Async is true"`
}

type logCloser func() error

func (closeLogs logCloser) Close() error { return closeLogs() }

// Logs installs application logging. Stop producers before closing the result.
// Use logger.FlushError() for an explicit delivery barrier during application use.
func Logs(flags *LogConfig, serviceName string) (io.Closer, error) {
	const maxAgeDays, maxSizeMB = 10, 10
	var destination io.Writer = os.Stderr
	var file *lumberjack.Logger
	if flags.LogDir != "" && flags.LogDir != os.DevNull {
		if err := os.MkdirAll(flags.LogDir, 0755); err != nil {
			return nil, errors.WithMessage(err, "unable to create log directory")
		}
		file = &lumberjack.Logger{
			Filename: filepath.Join(flags.LogDir, serviceName+".log"),
			MaxAge:   maxAgeDays,
			MaxSize:  maxSizeMB,
		}
		destination = file
		if flags.LogStd {
			destination = io.MultiWriter(file, os.Stderr)
		}
	}
	closeFile := func() error {
		if file == nil {
			return nil
		}
		return errors.WithMessage(file.Close(), "unable to close log file")
	}
	// The sink moves destination writes onto one worker and batches them.
	// Records are rendered by the producer, so nothing is encoded twice.
	var sink *xlog.Sink
	options := []xlog.FormatterOption{
		xlog.FormatWithCaller(true),
		xlog.FormatWithLocation(flags.LogDebug),
	}
	if flags.Async {
		var err error
		sink, err = xlog.NewSink(flags.BufSize)
		if err != nil {
			return nil, errors.CombineErrors(err, closeFile())
		}
		options = append(options, xlog.WithSink(sink))
	}
	var formatter xlog.Formatter
	switch {
	case flags.LogDir == os.DevNull:
		formatter = xlog.NewNilFormatter()
	case flags.LogStackdriver:
		formatter = stackdriver.NewFormatter(destination, serviceName, options...)
	case flags.LogJSON:
		formatter = xlog.NewJSONFormatter(destination, options...)
	case flags.LogPretty:
		formatter = xlog.NewPrettyFormatter(destination,
			append(options, xlog.FormatWithColor(file == nil))...)
	default:
		formatter = xlog.NewStringFormatter(destination, options...)
	}
	remove := xlog.InstallFormatter(formatter)
	return logCloser(sync.OnceValue(func() error {
		remove()
		err := xlog.FlushFormatter(formatter)
		if sink != nil {
			err = errors.CombineErrors(err, sink.Close())
		}
		return errors.CombineErrors(err, closeFile())
	})), nil
}

// application-example:end

func TestApplicationLogSetup(t *testing.T) {
	for _, async := range []bool{false, true} {
		for _, format := range []string{"text", "pretty", "json", "stackdriver", "nil"} {
			t.Run(fmt.Sprintf("%s/async=%t", format, async), func(t *testing.T) {
				dir := t.TempDir()
				flags := &LogConfig{
					Async:          async,
					BufSize:        2,
					LogDir:         dir,
					LogDebug:       true,
					LogPretty:      format == "pretty",
					LogJSON:        format == "json",
					LogStackdriver: format == "stackdriver",
				}
				if format == "nil" {
					flags.LogDir = os.DevNull
				}
				closer, err := Logs(flags, "service")
				require.NoError(t, err)
				l := xlog.NewPackageLogger("sink-example", "main")
				l.KV(xlog.INFO, "status", "service_starting")
				require.NoError(t, l.FlushError())
				require.NoError(t, closer.Close())
				require.NoError(t, closer.Close())
				if format != "nil" {
					data, err := os.ReadFile(filepath.Join(dir, "service.log"))
					require.NoError(t, err)
					require.Contains(t, string(data), "service_starting")
				}
			})
		}
	}
	_, err := Logs(&LogConfig{Async: true}, "invalid")
	require.Error(t, err)
}
