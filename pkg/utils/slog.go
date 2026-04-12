package utils

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NewConsoleLogger returns a compact, human-friendly terminal logger.
func NewConsoleLogger(w io.Writer, level slog.Leveler) *slog.Logger {
	return slog.New(&consoleHandler{
		writer: w,
		level:  level,
		state:  &consoleHandlerState{},
	})
}

type consoleHandler struct {
	writer io.Writer
	level  slog.Leveler
	attrs  []slog.Attr
	group  string
	state  *consoleHandlerState
}

type consoleHandlerState struct {
	mu sync.Mutex
}

func (h *consoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	if h.level == nil {
		return level >= slog.LevelInfo
	}
	return level >= h.level.Level()
}

func (h *consoleHandler) Handle(_ context.Context, record slog.Record) error {
	attrs := append([]slog.Attr{}, h.attrs...)
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})

	var line strings.Builder
	line.WriteString(record.Time.Format("2006-01-02 15:04:05"))
	line.WriteByte(' ')
	line.WriteString(levelLabel(record.Level))
	line.WriteByte(' ')
	line.WriteString(record.Message)

	for _, attr := range attrs {
		appendAttr(&line, h.group, attr)
	}
	line.WriteByte('\n')

	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	_, err := io.WriteString(h.writer, line.String())
	return err
}

func (h *consoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

func (h *consoleHandler) WithGroup(name string) slog.Handler {
	next := *h
	if h.group == "" {
		next.group = name
	} else {
		next.group = h.group + "." + name
	}
	return &next
}

func appendAttr(line *strings.Builder, group string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}
	key := attr.Key
	if group != "" && key != "" {
		key = group + "." + key
	}
	switch attr.Value.Kind() {
	case slog.KindGroup:
		for _, item := range attr.Value.Group() {
			appendAttr(line, key, item)
		}
	default:
		if key == "" {
			return
		}
		line.WriteByte(' ')
		line.WriteString(key)
		line.WriteByte('=')
		line.WriteString(formatValue(attr.Value))
	}
}

func formatValue(value slog.Value) string {
	switch value.Kind() {
	case slog.KindString:
		text := value.String()
		if text == "" {
			return `""`
		}
		if strings.ContainsAny(text, " \t\r\n=") {
			return strconv.Quote(text)
		}
		return text
	case slog.KindTime:
		return value.Time().Format(time.RFC3339)
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindBool:
		if value.Bool() {
			return "true"
		}
		return "false"
	case slog.KindInt64:
		return strconv.FormatInt(value.Int64(), 10)
	case slog.KindUint64:
		return strconv.FormatUint(value.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.FormatFloat(value.Float64(), 'f', -1, 64)
	default:
		return fmt.Sprint(value.Any())
	}
}

func levelLabel(level slog.Level) string {
	switch {
	case level <= slog.LevelDebug:
		return "DBG"
	case level < slog.LevelWarn:
		return "INF"
	case level < slog.LevelError:
		return "WRN"
	default:
		return "ERR"
	}
}
