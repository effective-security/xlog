[![Coverage Status](https://coveralls.io/repos/github/effective-security/xlog/badge.svg?branch=main)](https://coveralls.io/github/effective-security/xlog?branch=main)

# xlog logging package

Cloned from https://github.com/coreos/pkg/tree/master/capnslog

This clone has slight modifications on the original code,
adding ability to specify log lever per package,
and exposing `Logger` interface, not an implementation structure.

In this implementation the `DEBUG` level is above `TRACE` as trace
is used to trace important functions calls and maybe enable on the cloud more friequently than `DEBUG`

## How to use

```go
var logger = xlog.NewPackageLogger("github.com/yourorg/yourrepo", "yourpackage")

logger.KV(xlog.INFO, "version", v1, "any", override)
```

## How to configure

```go
	if withStackdriver {
		formatter := stackdriver.NewFormatter(os.Stderr, cfg.Logs.LogsName)
		xlog.SetFormatter(formatter)
	} else {
		formatter := xlog.NewColorFormatter(os.Stderr, true)
		xlog.SetFormatter(formatter)
	}
```

## Set log level for different packages

Config example:

```yaml
log_levels: 
  - repo: "*"
    level: INFO
  - repo: github.com/effective-security/server
    package: "*"
    level: TRACE
```

Configuration at start up:

```go
	// Set log levels for each repo
	if cfg.LogLevels != nil {
		for _, ll := range cfg.LogLevels {
			l, _ := xlog.ParseLevel(ll.Level)
			if ll.Repo == "*" {
				xlog.SetGlobalLogLevel(l)
			} else {
				xlog.SetPackageLogLevel(ll.Repo, ll.Package, l)
			}
			logger.Infof("logger=%q, level=%v", ll.Repo, l)
		}
	}
```

## Need to log to files?

This example shows how to use with `logrotate` package

```go
	if cfg.Logs.Directory != "" && cfg.Logs.Directory != nullDevName {
		os.MkdirAll(cfg.Logs.Directory, 0644)

		var sink io.Writer
		if flags.isStderr {
			// This will allow to also print the logs on stderr
			sink = os.Stderr
			xlog.SetFormatter(xlog.NewColorFormatter(sink, true))
		} else {
			// do not redirect stderr to our log files
			log.SetOutput(os.Stderr)
		}

		logRotate, err := logrotate.Initialize(cfg.Logs.Directory, cfg.ServiceName, cfg.Logs.MaxAgeDays, cfg.Logs.MaxSizeMb, true, sink)
		if err != nil {
			logger.Errorf("reason=logrotate, folder=%q, err=[%+v]", cfg.Logs.Directory, err)
			return errors.WithMessage(err, "failed to initialize log rotate")
		}
		// Close logRotate when application terminates
		app.OnClose(logRotate)
	}
```

## Design Principles

### `package main` is the place where logging gets turned on and routed

A library should not touch log options, only generate log entries. Libraries are silent until main lets them speak.

### All log options are runtime-configurable

Still the job of `main` to expose these configurations. `main` may delegate this to, say, a configuration webhook, but does so explicitly.

### There is one log object per package. It is registered under its repository and package name

`main` activates logging for its repository and any dependency repositories it would also like to have output in its logstream. `main` also dictates at which level each subpackage logs.

### There is *one* output stream, and it is an `io.Writer` composed with a formatter

Splitting streams is probably not the job of your program, but rather, your log aggregation framework. If you must split output streams, again, `main` configures this and you can write a very simple two-output struct that satisfies io.Writer.

Fancy colorful formatting and JSON output are beyond the scope of a basic logging framework -- they're application/log-collector dependant. These are, at best, provided as options, but more likely, provided by your application.

### Log objects are an interface

An object knows best how to print itself. Log objects can collect more interesting metadata if they wish, however, because text isn't going away anytime soon, they must all be marshalable to text. The simplest log object is a string, which returns itself. If you wish to do more fancy tricks for printing your log objects, see also JSON output -- introspect and write a formatter which can handle your advanced log interface. Making strings is the only thing guaranteed.

### Log levels have specific meanings:

* `CRITICAL`: Unrecoverable. Must fail.
* `ERROR`: Data has been lost, a request has failed for a bad reason, or a required resource has been lost
* `WARNING`: (Hopefully) Temporary conditions that may cause errors, but may work fine. A replica disappearing (that may reconnect) is a warning.
* `NOTICE`: Normal, but important (uncommon) log information.
* `INFO`: Normal, working log information, everything is fine, but helpful notices for auditing or common operations.
* `TRACE`: Anything goes, from logging every function call as part of a common operation, to tracing execution of a query.
* `DEBUG`: Print debug data.
