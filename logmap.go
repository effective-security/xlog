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
	"sync/atomic"

	"github.com/cockroachdb/errors"
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

// RepoLogger maps package names within one repository to their registered loggers.
// Handles returned by GetRepoLogger alias the registry; do not mutate or iterate
// them concurrently with logger registration or configuration.
type RepoLogger map[string]*PackageLogger

// OnErrorFn observes ERROR calls, including filtered ones. It runs synchronously
// without logger locks. Callbacks may use xlog configuration or log at other
// levels; logging ERROR recursively requires a caller-provided recursion guard.
// Concurrent ERROR calls may invoke the observer concurrently.
type OnErrorFn func(pkg string)

type loggerStruct struct {
	sync.Mutex
	repoMap   map[string]RepoLogger
	formatter Formatter
	current   *formatterRegistration
	// output serializes formatters that are not safe for concurrent use.
	// Formatters delivering through a Sink share it instead, so producers
	// render records in parallel and never hold it across queue admission.
	output sync.RWMutex
	// onError is read on every ERROR call, including filtered ones, so it is
	// published atomically instead of under the configuration lock.
	onError atomic.Pointer[OnErrorFn]
}

// logger is the global logger
var logger = new(loggerStruct)

// OnError installs an ERROR observer, or removes it when fn is nil.
// The callback runs synchronously without holding configuration or output locks.
func OnError(fn OnErrorFn) {
	if fn == nil {
		logger.onError.Store(nil)
		return
	}
	logger.onError.Store(&fn)
}

// currentOnError returns the installed ERROR observer without taking a lock.
func currentOnError() OnErrorFn {
	if observer := logger.onError.Load(); observer != nil {
		return *observer
	}
	return nil
}

// ConcurrentFormatter reports whether a formatter renders and delivers records
// without external serialization. PackageLogger stops serializing output for
// formatters that answer true, so their records are ordered by admission rather
// than by call order, preserving per-goroutine order. Formatters that embed
// Output answer true exactly when a Sink owns their destination.
//
// Concurrent formatters exclude formatters that need exclusive output, not each
// other, so during an InstallFormatter handoff two concurrent formatters can be
// active at once. Their sinks must therefore not share one destination that is
// unsafe for concurrent writes.
type ConcurrentFormatter interface {
	Formatter
	// Concurrent reports whether concurrent Format/FormatKV calls are safe.
	Concurrent() bool
}

// acquireOutput takes the output lock that f requires and reports whether it is
// shared. A concurrent formatter needs to exclude only formatters that require
// exclusive output, so it takes the lock for reading and runs in parallel with
// other concurrent formatters.
func acquireOutput(f Formatter) bool {
	if concurrent, ok := f.(ConcurrentFormatter); ok && concurrent.Concurrent() {
		logger.output.RLock()
		return true
	}
	logger.output.Lock()
	return false
}

func releaseOutput(shared bool) {
	if shared {
		logger.output.RUnlock()
		return
	}
	logger.output.Unlock()
}

// SetGlobalLogLevel sets the log level for all packages in all repositories
// already registered with NewPackageLogger. It does not set the default for
// future registrations. Derived loggers share their registered parent's level.
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
		v.level.Store(int32(l))
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
		l.level.Store(int32(v))
	}
}

// SetFormatter replaces the global formatter. Nil disables output. The old
// formatter is neither flushed nor closed; callers own its destination lifecycle.
func SetFormatter(f Formatter) {
	logger.Lock()
	defer logger.Unlock()
	logger.formatter = f
	logger.current = &formatterRegistration{formatter: f}
}

// formatterRegistration tracks admitted calls and removable formatter overrides.
// The linked list and WaitGroup admission are protected by logger.Mutex.
type formatterRegistration struct {
	formatter Formatter
	previous  *formatterRegistration
	active    sync.WaitGroup
}

// InstallFormatter temporarily installs f and returns an idempotent removal
// function. Overrides may be removed in any order; removed formatters are never
// restored by later removals. SetFormatter supersedes all installed overrides.
// Removal waits for admitted PackageLogger calls to finish, allowing the caller
// to flush and close f's destination safely. It does not flush or close f itself.
// Do not remove an override from inside its formatter or destination, or reuse
// its formatter elsewhere while closing it. Direct formatter calls are untracked.
func InstallFormatter(f Formatter) func() {
	logger.Lock()
	if logger.current == nil {
		logger.current = &formatterRegistration{formatter: logger.formatter}
	}
	registration := &formatterRegistration{
		formatter: f,
		previous:  logger.current,
	}
	logger.current = registration
	logger.formatter = f
	logger.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			logger.Lock()
			for link := &logger.current; *link != nil; link = &(*link).previous {
				if *link == registration {
					*link = registration.previous
					break
				}
			}
			logger.formatter = logger.current.formatter
			logger.Unlock()
			registration.active.Wait()
		})
	}
}

// GetFormatter returns current formatter
func GetFormatter() Formatter {
	logger.Lock()
	defer logger.Unlock()
	return logger.formatter
}

// NewPackageLogger creates a package logger object.
// This should be defined as a global var in your package, referencing your repo.
// Repeated calls for the same repo and pkg return the same pointer. New loggers
// start at INFO, regardless of earlier global or repository level settings.
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
		created := &PackageLogger{
			pkg: pkg,
		}
		created.level.Store(int32(INFO))
		r[pkg] = created
		p = created
	}
	return
}

// getRepoLogger adds repository context to registry lookup errors.
func getRepoLogger(repo string) (RepoLogger, error) {
	repoLogger, err := GetRepoLogger(repo)
	if err != nil {
		return nil, errors.WithMessagef(err, "failed to get repo logger: %s", repo)
	}
	return repoLogger, nil
}

// SetRepoLogLevel updates registered packages in repo; unknown repositories are ignored.
func SetRepoLogLevel(repo string, l LogLevel) {
	if logger, err := getRepoLogger(repo); err == nil {
		logger.SetRepoLogLevel(l)
	}
}

// SetPackageLogLevel updates a registered package. An empty pkg or "*" selects
// all registered packages in repo. Unknown repositories and packages are ignored.
func SetPackageLogLevel(repo, pkg string, l LogLevel) {
	if pkg == "" || pkg == "*" {
		SetRepoLogLevel(repo, l)
		return
	}

	if pkgLogger, err := getRepoLogger(repo); err == nil {
		logger.Lock()
		defer logger.Unlock()

		if p, ok := pkgLogger[pkg]; ok {
			p.level.Store(int32(l))
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

// SetRepoLevel applies a configuration entry. Invalid levels currently become
// CRITICAL because parsing errors are ignored; validate with ParseLevel first.
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
				Level:   LogLevel(rl.level.Load()).String(),
			})
		}
	}

	return list
}
