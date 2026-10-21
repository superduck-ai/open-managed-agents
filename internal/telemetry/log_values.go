package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
)

type scalarValueHandler struct {
	next slog.Handler
}

func newScalarValueHandler(next slog.Handler) slog.Handler {
	return scalarValueHandler{next: next}
}

func (h scalarValueHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h scalarValueHandler) Handle(ctx context.Context, record slog.Record) error {
	converted := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		converted.AddAttrs(scalarAttr(attr))
		return true
	})
	return h.next.Handle(ctx, converted)
}

func (h scalarValueHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	converted := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		converted[i] = scalarAttr(attr)
	}
	return scalarValueHandler{next: h.next.WithAttrs(converted)}
}

func (h scalarValueHandler) WithGroup(name string) slog.Handler {
	return scalarValueHandler{next: h.next.WithGroup(name)}
}

func scalarAttr(attr slog.Attr) slog.Attr {
	value := attr.Value.Resolve()
	switch value.Kind() {
	case slog.KindGroup:
		group := value.Group()
		converted := make([]any, len(group))
		for i, member := range group {
			converted[i] = scalarAttr(member)
		}
		return slog.Group(attr.Key, converted...)
	case slog.KindAny:
		return slog.Attr{Key: attr.Key, Value: scalarValue(value)}
	default:
		return slog.Attr{Key: attr.Key, Value: value}
	}
}

func scalarValue(value slog.Value) slog.Value {
	raw := value.Any()
	if _, ok := raw.(error); ok {
		return value
	}
	if stringer, ok := raw.(fmt.Stringer); ok {
		return slog.StringValue(stringer.String())
	}
	reflected := reflect.ValueOf(raw)
	switch reflected.Kind() {
	case reflect.String:
		return slog.StringValue(reflected.String())
	case reflect.Bool:
		return slog.BoolValue(reflected.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return slog.Int64Value(reflected.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return slog.Uint64Value(reflected.Uint())
	case reflect.Float32, reflect.Float64:
		return slog.Float64Value(reflected.Float())
	default:
		return value
	}
}
