//go:build windows

package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/svc/eventlog"
)

type windowsEventLogHandler struct {
	elog   *eventlog.Log
	level  slog.Leveler
	attrs  []slog.Attr
	groups []string
}

func newWindowsEventLogHandler(elog *eventlog.Log, level slog.Leveler) slog.Handler {
	return &windowsEventLogHandler{
		elog:  elog,
		level: level,
	}
}

func (h *windowsEventLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	_ = ctx
	minLevel := slog.LevelInfo
	if h.level != nil {
		minLevel = h.level.Level()
	}
	return level >= minLevel
}

func (h *windowsEventLogHandler) Handle(ctx context.Context, r slog.Record) error {
	if !h.Enabled(ctx, r.Level) {
		return nil
	}

	groupPrefix := strings.Join(h.groups, ".")
	if groupPrefix != "" {
		groupPrefix += "."
	}

	var b strings.Builder
	if !r.Time.IsZero() {
		b.WriteString("time=")
		b.WriteString(r.Time.Format("2006-01-02T15:04:05.000Z07:00"))
		b.WriteString(" ")
	}
	b.WriteString("level=")
	b.WriteString(r.Level.String())
	b.WriteString(" msg=")
	b.WriteString(strconv.Quote(r.Message))

	attrs := make([]slog.Attr, 0, len(h.attrs)+6)
	attrs = append(attrs, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	for _, a := range attrs {
		if a.Key == "" {
			continue
		}
		b.WriteString(" ")
		b.WriteString(groupPrefix)
		b.WriteString(a.Key)
		b.WriteString("=")
		b.WriteString(attrValueString(a.Value))
	}
	msg := strings.TrimSpace(b.String())

	switch {
	case r.Level >= slog.LevelError:
		return h.elog.Error(1, msg)
	case r.Level >= slog.LevelWarn:
		return h.elog.Warning(2, msg)
	default:
		return h.elog.Info(3, msg)
	}
}

func (h *windowsEventLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	next.attrs = append(next.attrs, h.attrs...)
	next.attrs = append(next.attrs, attrs...)
	return &next
}

func (h *windowsEventLogHandler) WithGroup(name string) slog.Handler {
	if strings.TrimSpace(name) == "" {
		return h
	}
	next := *h
	next.groups = make([]string, 0, len(h.groups)+1)
	next.groups = append(next.groups, h.groups...)
	next.groups = append(next.groups, name)
	return &next
}

func attrValueString(v slog.Value) string {
	v = v.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return strconv.Quote(v.String())
	case slog.KindBool:
		if v.Bool() {
			return "true"
		}
		return "false"
	case slog.KindInt64:
		return strconv.FormatInt(v.Int64(), 10)
	case slog.KindUint64:
		return strconv.FormatUint(v.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.FormatFloat(v.Float64(), 'f', -1, 64)
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().Format("2006-01-02T15:04:05.000Z07:00")
	default:
		return strconv.Quote(fmt.Sprint(v.Any()))
	}
}
