package xlog

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNilLoggerMethods(t *testing.T) {
	t.Parallel()
	logger := NewNilLogger()

	noPanic := []struct {
		name string
		fn   func()
	}{
		{"Fatal", func() { logger.Fatal("msg") }},
		{"Fatalf", func() { logger.Fatalf("%s", "msg") }},
		{"Info", func() { logger.Info("msg") }},
		{"Infof", func() { logger.Infof("%s", "msg") }},
		{"KV", func() { logger.KV(INFO, "k", "v") }},
		{"ContextKV", func() { logger.ContextKV(context.Background(), DEBUG, "k", "v") }},
		{"Error", func() { logger.Error("msg") }},
		{"Errorf", func() { logger.Errorf("%s", "msg") }},
		{"Warning", func() { logger.Warning("msg") }},
		{"Warningf", func() { logger.Warningf("%s", "msg") }},
		{"Notice", func() { logger.Notice("msg") }},
		{"Noticef", func() { logger.Noticef("%s", "msg") }},
		{"Debug", func() { logger.Debug("msg") }},
		{"Debugf", func() { logger.Debugf("%s", "msg") }},
		{"Trace", func() { logger.Trace("msg") }},
		{"Tracef", func() { logger.Tracef("%s", "msg") }},
		{"WithValues", func() { _ = logger.WithValues("k", "v") }},
	}
	for _, tc := range noPanic {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.NotPanics(t, tc.fn)
		})
	}

	panicCases := []struct {
		name string
		fn   func()
	}{
		{"Panic", func() { logger.Panic("msg") }},
		{"Panicf", func() { logger.Panicf("%s", "msg") }},
	}
	for _, tc := range panicCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Panics(t, tc.fn)
		})
	}
}
