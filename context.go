package xlog

import (
	"context"
	"sort"
	"sync"
)

type contextKey int

const (
	keyContext contextKey = iota
)

// contextLogs represents extra data in the Context that will be added to logs, in key=value format
type contextLogs struct {
	entries []any
	kvMap   map[string]any // internal map for efficient key-value operations
	lock    sync.RWMutex
}

// ContextWithKV returns context with values to be added to logs,
// entries in "key1=value1, ..., keyN=valueN" format.
// The entries must come in pairs of key (string) and value;
// an odd number of entries or a non-string key will cause a panic.
// The nil or empty string values are ignored.
// If ContextWithKV is called multiple times, the values are accumulated and/or updated.
// If a key is provided multiple times, the latest value is used.
func ContextWithKV(ctx context.Context, entries ...any) context.Context {
	if len(entries)%2 != 0 {
		// it's safe to panic here, instead of during logging
		panic("entries must be in key=value pairs")
	}

	v := ctx.Value(keyContext)
	var rctx *contextLogs

	if v == nil {
		rctx = &contextLogs{
			entries: make([]any, 0),
			kvMap:   make(map[string]any),
		}
		// Only call context.WithValue when creating new context
		ctx = context.WithValue(ctx, keyContext, rctx)
	} else {
		rctx = v.(*contextLogs)
		rctx.lock.Lock()
		defer rctx.lock.Unlock()
	}

	// Process entries in pairs
	for i := 0; i < len(entries); i += 2 {
		key, ok := entries[i].(string)
		if !ok {
			panic("keys must be strings")
		}

		value := entries[i+1]

		// Skip nil or empty string values
		if value == nil {
			delete(rctx.kvMap, key)
			continue
		}
		if strVal, isStr := value.(string); isStr && strVal == "" {
			delete(rctx.kvMap, key)
			continue
		}

		// Store the key-value pair (latest value wins for duplicate keys)
		rctx.kvMap[key] = value
	}

	// Recompute the entries slice for fast retrieval in deterministic order
	rctx.entries = make([]any, 0, len(rctx.kvMap)*2)

	// Collect keys and sort them for deterministic order
	keys := make([]string, 0, len(rctx.kvMap))
	for key := range rctx.kvMap {
		keys = append(keys, key)
	}

	// Sort keys for deterministic iteration order
	sort.Strings(keys)

	// Build entries slice in sorted order
	for _, key := range keys {
		rctx.entries = append(rctx.entries, key, rctx.kvMap[key])
	}

	return ctx
}

// ContextEntries returns log entries as a slice of alternating keys and values
func ContextEntries(ctx context.Context) []any {
	v := ctx.Value(keyContext)
	if v == nil {
		return nil
	}

	rctx := v.(*contextLogs)
	rctx.lock.RLock()
	defer rctx.lock.RUnlock()
	return rctx.entries
}
