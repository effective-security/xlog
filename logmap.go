// Copyright 2015 CoreOS, Inc.
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

package xlog

import (
	"strings"
	"sync"

	"github.com/pkg/errors"
)

// LogLevel is the set of all log levels.
type LogLevel int8

const (
	// CRITICAL is the lowest log level; only errors which will end the program will be propagated.
	CRITICAL LogLevel = iota - 1
	// ERROR is for errors that are not fatal but lead to troubling behavior.
	ERROR
	// WARNING is for errors which are not fatal and not errors, but are unusual. Often sourced from misconfigurations.
	WARNING
	// NOTICE is for normal but significant conditions.
	NOTICE
	// INFO is a log level for common, everyday log updates.
	INFO
	// TRACE is for (potentially) call by call tracing of programs.
	TRACE
	// DEBUG is the default hidden level for more verbose updates about internal processes.
	DEBUG
)

// Char returns a single-character representation of the log level.
func (l LogLevel) Char() string {
	switch l {
	case CRITICAL:
		return "C"
	case ERROR:
		return "E"
	case WARNING:
		return "W"
	case NOTICE:
		return "N"
	case INFO:
		return "I"
	case TRACE:
		return "T"
	case DEBUG:
		return "D"
	default:
		panic("Unhandled loglevel")
	}
}

// String returns a multi-character representation of the log level.
func (l LogLevel) String() string {
	switch l {
	case CRITICAL:
		return "CRITICAL"
	case ERROR:
		return "ERROR"
	case WARNING:
		return "WARNING"
	case NOTICE:
		return "NOTICE"
	case INFO:
		return "INFO"
	case TRACE:
		return "TRACE"
	case DEBUG:
		return "DEBUG"
	default:
		panic("Unhandled loglevel")
	}
}

// Set the log level
func (l *LogLevel) Set(s string) error {
	value, err := ParseLevel(s)
	if err != nil {
		return errors.WithStack(err)
	}
	*l = value
	return nil
}

// ParseLevel translates some potential loglevel strings into their corresponding levels.
func ParseLevel(s string) (LogLevel, error) {
	switch s {
	case "CRITICAL", "C":
		return CRITICAL, nil
	case "ERROR", "0", "E":
		return ERROR, nil
	case "WARNING", "1", "W":
		return WARNING, nil
	case "NOTICE", "2", "N":
		return NOTICE, nil
	case "INFO", "3", "I":
		return INFO, nil
	case "TRACE", "4", "T":
		return TRACE, nil
	case "DEBUG", "5", "D":
		return DEBUG, nil
	}
	return CRITICAL, errors.New("unable to parse log level: " + s)
}

// RepoLogger specifies a map of repo => PackageLogger
type RepoLogger map[string]*PackageLogger

// OnErrorFn allows to be called when an error is logged in a package
type OnErrorFn func(pkg string)

type loggerStruct struct {
	sync.Mutex
	repoMap   map[string]RepoLogger
	formatter Formatter
	onError   OnErrorFn
}

// logger is the global logger
var logger = new(loggerStruct)

// OnError allows to specify a callback for ERROR levels.
// This is useful to reports metrics on ERROR in a package
func OnError(fn OnErrorFn) {
	logger.Lock()
	defer logger.Unlock()
	logger.onError = fn
}

// SetGlobalLogLevel sets the log level for all packages in all repositories
// registered with PackageLogger.
func SetGlobalLogLevel(l LogLevel) {
	logger.Lock()
	defer logger.Unlock()
	for _, r := range logger.repoMap {
		r.setRepoLogLevelInternal(l)
	}
}

// GetRepoLogger may return the handle to the repository's set of packages' loggers.
func GetRepoLogger(repo string) (RepoLogger, error) {
	logger.Lock()
	defer logger.Unlock()
	r, ok := logger.repoMap[repo]
	if !ok {
		return nil, errors.Errorf("no packages registered for repo: %s", repo)
	}
	return r, nil
}

// MustRepoLogger returns the handle to the repository's packages' loggers.
func MustRepoLogger(repo string) RepoLogger {
	r, err := GetRepoLogger(repo)
	if err != nil {
		panic(err)
	}
	return r
}

// SetRepoLogLevel sets the log level for all packages in the repository.
func (r RepoLogger) SetRepoLogLevel(l LogLevel) {
	logger.Lock()
	defer logger.Unlock()
	r.setRepoLogLevelInternal(l)
}

func (r RepoLogger) setRepoLogLevelInternal(l LogLevel) {
	for _, v := range r {
		v.level = l
	}
}

// ParseLogLevelConfig parses a comma-separated string of "package=loglevel", in
// order, and returns a map of the results, for use in SetLogLevel.
func (r RepoLogger) ParseLogLevelConfig(conf string) (map[string]LogLevel, error) {
	setlist := strings.Split(conf, ",")
	out := make(map[string]LogLevel)
	for _, setstring := range setlist {
		setting := strings.Split(setstring, "=")
		if len(setting) != 2 {
			return nil, errors.New("oddly structured `pkg=level` option: " + setstring)
		}
		l, err := ParseLevel(setting[1])
		if err != nil {
			return nil, errors.WithStack(err)
		}
		out[setting[0]] = l
	}
	return out, nil
}

// SetLogLevel takes a map of package names within a repository to their desired
// loglevel, and sets the levels appropriately. Unknown packages are ignored.
// "*" is a special package name that corresponds to all packages, and will be
// processed first.
func (r RepoLogger) SetLogLevel(m map[string]LogLevel) {
	logger.Lock()
	defer logger.Unlock()
	if l, ok := m["*"]; ok {
		r.setRepoLogLevelInternal(l)
	}
	for k, v := range m {
		l, ok := r[k]
		if !ok {
			continue
		}
		l.level = v
	}
}

// SetFormatter sets the formatting function for all logs.
func SetFormatter(f Formatter) {
	logger.Lock()
	defer logger.Unlock()
	logger.formatter = f
}

// GetFormatter returns current formatter
func GetFormatter() Formatter {
	logger.Lock()
	defer logger.Unlock()
	return logger.formatter
}

// NewPackageLogger creates a package logger object.
// This should be defined as a global var in your package, referencing your repo.
func NewPackageLogger(repo string, pkg string) (p *PackageLogger) {
	logger.Lock()
	defer logger.Unlock()
	if logger.repoMap == nil {
		logger.repoMap = make(map[string]RepoLogger)
	}
	r, rok := logger.repoMap[repo]
	if !rok {
		logger.repoMap[repo] = make(RepoLogger)
		r = logger.repoMap[repo]
	}
	p, pok := r[pkg]
	if !pok {
		r[pkg] = &PackageLogger{
			pkg:   pkg,
			level: INFO,
		}
		p = r[pkg]
	}
	return
}

// getRepoLogger wraps the call to capnlog.GetRepoLogger
func getRepoLogger(repo string) (RepoLogger, error) {
	repoLogger, err := GetRepoLogger(repo)
	if err != nil {
		return nil, errors.WithMessagef(err, "failed to get repo logger: %s", repo)
	}
	return repoLogger, nil
}

// SetRepoLogLevel sets the log level for all packages in repo logger
func SetRepoLogLevel(repo string, l LogLevel) {
	if logger, err := getRepoLogger(repo); err == nil {
		logger.SetRepoLogLevel(l)
	}
}

// SetPackageLogLevel sets the log level for a package in repo logger
func SetPackageLogLevel(repo, pkg string, l LogLevel) {
	if pkg == "" || pkg == "*" {
		SetRepoLogLevel(repo, l)
		return
	}

	if pkgLogger, err := getRepoLogger(repo); err == nil {
		logger.Lock()
		defer logger.Unlock()

		if p, ok := pkgLogger[pkg]; ok {
			p.level = l
		}
	}
}

// RepoLogLevel contains information about the log level per repo. Use * to set up global level.
type RepoLogLevel struct {
	// Repo specifies the repo name, or '*' for all repos [Global]
	Repo string `json:"repo,omitempty" yaml:"repo,omitempty"`
	// Package specifies the package name
	Package string `json:"package,omitempty" yaml:"package,omitempty"`
	// Level specifies the log level for the repo [ERROR,WARNING,NOTICE,INFO,DEBUG,TRACE].
	Level string `json:"level,omitempty" yaml:"level,omitempty"`
}

// SetRepoLevels sets repo log levels per package
func SetRepoLevels(cfg []RepoLogLevel) {
	for _, ll := range cfg {
		SetRepoLevel(ll)
	}
}

// SetRepoLevel sets repo log level
func SetRepoLevel(cfg RepoLogLevel) {
	l, _ := ParseLevel(cfg.Level)
	if cfg.Repo == "*" {
		SetGlobalLogLevel(l)
	} else {
		SetPackageLogLevel(cfg.Repo, cfg.Package, l)
	}
}

// GetRepoLevels returns currently configured levels
func GetRepoLevels() []RepoLogLevel {
	logger.Lock()
	defer logger.Unlock()

	list := make([]RepoLogLevel, 0, len(logger.repoMap))
	for repo, v := range logger.repoMap {
		for pkg, rl := range v {
			if pkg == "" {
				pkg = "*"
			}
			list = append(list, RepoLogLevel{
				Repo:    repo,
				Package: pkg,
				Level:   rl.level.String(),
			})
		}
	}

	return list
}
