// Package observability provides logging helpers, including a console slog
// handler that renders human-friendly, ANSI-colored output for local/dev use.
package observability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
)

const (
	ansiReset  = "\033[0m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiGray   = "\033[90m"
)

// ConsoleHandler writes colored, single-line logs. HTTP access logs
// (component=http) are rendered as "[clientKind] METHOD STATUS durationMs URL".
type ConsoleHandler struct {
	mu     *sync.Mutex
	w      io.Writer
	level  slog.Leveler
	attrs  []slog.Attr
	groups []string
}

func NewConsoleHandler(w io.Writer, level slog.Leveler) *ConsoleHandler {
	if level == nil {
		level = slog.LevelInfo
	}
	return &ConsoleHandler{mu: &sync.Mutex{}, w: w, level: level}
}

func (h *ConsoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *ConsoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	nh := h.clone()
	prefix := strings.Join(h.groups, ".")
	for _, a := range attrs {
		nh.attrs = append(nh.attrs, qualifyAttr(prefix, a))
	}
	return nh
}

func (h *ConsoleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	nh := h.clone()
	nh.groups = append(nh.groups, name)
	return nh
}

func (h *ConsoleHandler) clone() *ConsoleHandler {
	return &ConsoleHandler{
		mu:     h.mu,
		w:      h.w,
		level:  h.level,
		attrs:  append([]slog.Attr{}, h.attrs...),
		groups: append([]string{}, h.groups...),
	}
}

func (h *ConsoleHandler) Handle(_ context.Context, r slog.Record) error {
	values := map[string]string{}
	order := make([]string, 0, len(h.attrs)+r.NumAttrs())
	add := func(key, val string) {
		if key == "" {
			return
		}
		if _, ok := values[key]; !ok {
			order = append(order, key)
		}
		values[key] = val
	}
	for _, a := range h.attrs {
		add(a.Key, attrValueString(a.Value))
	}
	prefix := strings.Join(h.groups, ".")
	r.Attrs(func(a slog.Attr) bool {
		qa := qualifyAttr(prefix, a)
		add(qa.Key, attrValueString(qa.Value))
		return true
	})

	var line string
	if values["component"] == "http" {
		line = formatHTTPLine(r, values, order)
	} else {
		line = formatGenericLine(r, values, order)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, line+"\n")
	return err
}

func formatHTTPLine(r slog.Record, values map[string]string, order []string) string {
	base := ""
	if r.Level >= slog.LevelError || statusIsError(values["status"]) {
		base = ansiRed
	}

	var b strings.Builder
	b.WriteString(base)
	b.WriteString(ansiGray)
	b.WriteString(r.Time.Format("15:04:05.000"))
	b.WriteString(resetTo(base))
	b.WriteString(" [")
	b.WriteString(values["clientKind"])
	b.WriteString("] ")
	b.WriteString(values["method"])
	if status := values["status"]; status != "" {
		b.WriteString(" ")
		b.WriteString(status)
	}
	if duration := values["durationMs"]; duration != "" {
		b.WriteString(" ")
		b.WriteString(duration)
		b.WriteString("ms")
	}
	b.WriteString(" ")
	b.WriteString(values["url"])

	skip := map[string]bool{
		"component": true, "event": true, "clientKind": true,
		"method": true, "status": true, "durationMs": true, "url": true,
	}
	appendRest(&b, values, order, skip)

	b.WriteString(ansiReset)
	return b.String()
}

func formatGenericLine(r slog.Record, values map[string]string, order []string) string {
	base := ""
	if r.Level >= slog.LevelError {
		base = ansiRed
	}

	var b strings.Builder
	b.WriteString(base)
	b.WriteString(ansiGray)
	b.WriteString(r.Time.Format("15:04:05.000"))
	b.WriteString(resetTo(base))
	b.WriteString(" ")
	b.WriteString(levelColor(r.Level))
	b.WriteString(levelLabel(r.Level))
	b.WriteString(resetTo(base))
	if component := values["component"]; component != "" {
		b.WriteString(" [")
		b.WriteString(component)
		b.WriteString("]")
	}
	b.WriteString(" ")
	b.WriteString(r.Message)

	skip := map[string]bool{"component": true}
	appendRest(&b, values, order, skip)

	b.WriteString(ansiReset)
	return b.String()
}

func resetTo(base string) string {
	if base == "" {
		return ansiReset
	}
	return ansiReset + base
}

func appendRest(b *strings.Builder, values map[string]string, order []string, skip map[string]bool) {
	for _, key := range order {
		if skip[key] {
			continue
		}
		value := values[key]
		if value == "" {
			continue
		}
		b.WriteString(" ")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(maybeQuote(value))
	}
}

func levelLabel(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "ERROR"
	case level >= slog.LevelWarn:
		return "WARN"
	case level >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}

func levelColor(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return ansiRed
	case level >= slog.LevelWarn:
		return ansiYellow
	case level >= slog.LevelInfo:
		return ansiGreen
	default:
		return ansiCyan
	}
}

func statusIsError(status string) bool {
	return len(status) > 0 && (status[0] == '4' || status[0] == '5')
}

func attrValueString(v slog.Value) string {
	return v.Resolve().String()
}

func qualifyAttr(prefix string, a slog.Attr) slog.Attr {
	if prefix == "" {
		return a
	}
	return slog.Attr{Key: prefix + "." + a.Key, Value: a.Value}
}

func maybeQuote(value string) string {
	if strings.ContainsAny(value, " \t\"") {
		return fmt.Sprintf("%q", value)
	}
	return value
}
