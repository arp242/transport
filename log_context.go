package transport

import (
	"context"
	"log/slog"
)

var ctxkey = &struct{}{}

// LogContext adds key-value pairs as context values, which are logged in the
// [Log] transport.
//
// The key-value pairs work like the [slog] package (and accepts [slog.Attr]).
func LogContext(ctx context.Context, args ...any) context.Context {
	return context.WithValue(ctx, ctxkey, argsToAttrSlice(args))
}

func getLogContext(ctx context.Context) ([]slog.Attr, bool) {
	v := ctx.Value(ctxkey)
	vv, ok := v.([]slog.Attr)
	return vv, ok
}

// Copy from slog

func argsToAttrSlice(args []any) []slog.Attr {
	var (
		attr  slog.Attr
		attrs []slog.Attr
	)
	for len(args) > 0 {
		attr, args = argsToAttr(args)
		attrs = append(attrs, attr)
	}
	return attrs
}

const badKey = "!BADKEY"

func argsToAttr(args []any) (slog.Attr, []any) {
	switch x := args[0].(type) {
	case string:
		if len(args) == 1 {
			return slog.String(badKey, x), nil
		}
		return slog.Any(x, args[1]), args[2:]
	case slog.Attr:
		return x, args[1:]
	default:
		return slog.Any(badKey, x), args[1:]
	}
}
