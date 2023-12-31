package xlog

import "context"

type contextKey int

const (
	keyContext contextKey = iota
)

// contextLogs represents extra data in the Context that will be added to logs, in key=value format
type contextLogs struct {
	entries []any
}

// ContextWithKV returns context with values to be added to logs,
// entries in "key1=value1, ..., keyN=valueN" format.
func ContextWithKV(ctx context.Context, entries ...any) context.Context {
	v := ctx.Value(keyContext)
	if v == nil {
		rctx := &contextLogs{
			entries: entries,
		}
		ctx = context.WithValue(ctx, keyContext, rctx)
	} else {
		rctx := v.(*contextLogs)
		rctx.entries = append(rctx.entries, entries...)
	}
	return ctx
}

// ContextEntries returns log entries
func ContextEntries(ctx context.Context) []any {
	v := ctx.Value(keyContext)
	if v == nil {
		return nil
	}
	return v.(*contextLogs).entries
}
