package xlog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/effective-security/xlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDerivedLoggerOwnsFields(t *testing.T) {
	var out bytes.Buffer
	remove := xlog.InstallFormatter(xlog.NewJSONFormatter(&out).Options(
		xlog.FormatSkipTime(true), xlog.FormatWithCaller(false)))
	defer remove()
	p := xlog.NewPackageLogger(t.Name(), "fields")
	fields := make([]any, 2, 32)
	fields[0], fields[1] = "base", "original"
	base := p.WithValues(fields...).WithValues("a", 1, "b", 2).WithValues("c", 3).(*xlog.PackageLogger)
	fields[1] = "changed"
	one := base.WithValues("child", "one").(*xlog.PackageLogger)
	_ = base.WithValues("child", "two")
	base.KV(xlog.INFO, "child", "per-call")
	one.Info("hello")
	one.Infof("hello %d", 42)
	one.ContextKV(xlog.ContextWithKV(context.Background(), "context", true), xlog.INFO, "event", "check")
	decoder := json.NewDecoder(&out)
	for _, expected := range []map[string]any{
		{"base": "original", "child": "per-call"},
		{"base": "original", "child": "one", "msg": "hello"},
		{"base": "original", "child": "one", "msg": "hello 42"},
		{"base": "original", "child": "one", "context": true, "event": "check"},
	} {
		expected["level"], expected["pkg"] = "I", "fields"
		expected["a"], expected["b"], expected["c"] = float64(1), float64(2), float64(3)
		var actual map[string]any
		require.NoError(t, decoder.Decode(&actual))
		assert.Equal(t, expected, actual)
	}
}

func TestDerivedLoggerFollowsLevels(t *testing.T) {
	var out bytes.Buffer
	remove := xlog.InstallFormatter(xlog.NewJSONFormatter(&out))
	defer remove()
	repo := t.Name()
	p := xlog.NewPackageLogger(repo, "levels")
	derived := p.WithValues("field", true).WithValues("nested", true).(*xlog.PackageLogger)
	for _, set := range []func(xlog.LogLevel){
		func(level xlog.LogLevel) { xlog.SetPackageLogLevel(repo, "levels", level) },
		func(level xlog.LogLevel) { xlog.SetRepoLogLevel(repo, level) },
		xlog.MustRepoLogger(repo).SetRepoLogLevel,
		func(level xlog.LogLevel) {
			xlog.MustRepoLogger(repo).SetLogLevel(map[string]xlog.LogLevel{"levels": level})
		},
	} {
		set(xlog.CRITICAL)
		require.False(t, derived.LevelAt(xlog.INFO))
		derived.Info("filtered")
		require.Empty(t, out.String())
		set(xlog.DEBUG)
		require.True(t, derived.LevelAt(xlog.DEBUG))
		derived.Debug("enabled")
		require.Contains(t, out.String(), "enabled")
		out.Reset()
	}
	var workers sync.WaitGroup
	workers.Go(func() {
		for range 100 {
			xlog.SetRepoLogLevel(repo, xlog.INFO)
			xlog.SetRepoLogLevel(repo, xlog.CRITICAL)
		}
	})
	workers.Go(func() {
		for range 100 {
			p.WithValues("new", true).KV(xlog.INFO, "event", "derive")
			derived.Info("read shared level")
		}
	})
	workers.Wait()
}

func TestDerivedLoggerFollowsGlobalLevel(t *testing.T) {
	p := xlog.NewPackageLogger(t.Name(), "global")
	levels := xlog.GetRepoLevels()
	defer func() {
		// Restore wildcard defaults before explicit package overrides.
		for _, level := range levels {
			if level.Package == "*" {
				xlog.SetRepoLevel(level)
			}
		}
		for _, level := range levels {
			if level.Package != "*" {
				xlog.SetRepoLevel(level)
			}
		}
	}()
	remove := xlog.InstallFormatter(xlog.NewJSONFormatter(io.Discard))
	defer remove()
	derived := p.WithValues("field", true).(*xlog.PackageLogger)
	xlog.SetGlobalLogLevel(xlog.CRITICAL)
	require.False(t, derived.LevelAt(xlog.INFO))
	xlog.SetGlobalLogLevel(xlog.DEBUG)
	require.True(t, derived.LevelAt(xlog.DEBUG))
}

func TestContextEntriesSnapshot(t *testing.T) {
	ctx := xlog.ContextWithKV(context.Background(), "value", 1)
	entries := xlog.ContextEntries(ctx)
	entries[1] = 99
	require.Equal(t, []any{"value", 1}, xlog.ContextEntries(ctx))
	var workers sync.WaitGroup
	workers.Go(func() {
		for range 100 {
			xlog.ContextWithKV(ctx, "value", 2)
		}
	})
	workers.Go(func() {
		for range 100 {
			snapshot := xlog.ContextEntries(ctx)
			snapshot[1] = 99
		}
	})
	workers.Wait()
	require.Equal(t, []any{"value", 2}, xlog.ContextEntries(ctx))
}

type callbackWriter func([]byte) (int, error)

func (fn callbackWriter) Write(b []byte) (int, error) { return fn(b) }

func TestObserverCanReenterConfiguration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out bytes.Buffer
		remove := xlog.InstallFormatter(xlog.NewJSONFormatter(&out))
		defer remove()
		p := xlog.NewPackageLogger(t.Name(), "observer")
		defer xlog.SetRepoLogLevel(t.Name(), xlog.INFO)
		calls := 0
		xlog.OnError(func(string) {
			calls++
			require.NotNil(t, xlog.GetFormatter())
			p.Info("observer message")
		})
		defer xlog.OnError(nil)
		p.Error("outer message")
		xlog.SetRepoLogLevel(t.Name(), xlog.CRITICAL)
		p.Errorf("filtered message")
		require.Equal(t, 2, calls)
		require.Contains(t, out.String(), "observer message")
		require.NotContains(t, out.String(), "filtered message")
	})
}

func TestBlockedOutputDoesNotBlockConfiguration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		writer := callbackWriter(func(b []byte) (int, error) {
			require.NotNil(t, xlog.GetFormatter())
			close(entered)
			<-release
			return len(b), nil
		})
		remove := xlog.InstallFormatter(xlog.NewPrettyFormatter(writer))
		p := xlog.NewPackageLogger(t.Name(), "blocked")
		defer xlog.SetRepoLogLevel(t.Name(), xlog.INFO)
		go p.Info("blocked")
		<-entered
		xlog.SetRepoLogLevel(t.Name(), xlog.CRITICAL)
		p.Info("filtered")
		require.False(t, p.LevelAt(xlog.INFO))
		done := make(chan struct{})
		go func() { remove(); close(done) }()
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("formatter removal returned before admitted output finished")
		default:
		}
		close(release)
		<-done
	})
}

func TestFormatterOverridesAndNilFlush(t *testing.T) {
	old := xlog.GetFormatter()
	defer xlog.SetFormatter(old)
	xlog.SetFormatter(nil)
	p := xlog.NewPackageLogger(t.Name(), "nil")
	require.NotPanics(t, p.Flush)
	first := xlog.NewNilFormatter()
	removeFirst := xlog.InstallFormatter(first)
	second := xlog.NewNilFormatter()
	removeSecond := xlog.InstallFormatter(second)
	removeFirst()
	require.Same(t, second, xlog.GetFormatter())
	removeSecond()
	require.Nil(t, xlog.GetFormatter())
	removeFirst()
	removeSecond()
	remove := xlog.InstallFormatter(first)
	xlog.SetFormatter(second)
	remove()
	require.Same(t, second, xlog.GetFormatter())
}
