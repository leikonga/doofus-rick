package logbuf

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

const maxEntries = 50

type Buffer struct {
	mu      sync.Mutex
	entries [maxEntries]string
	pos     int
	count   int
}

func (b *Buffer) add(s string) {
	b.mu.Lock()
	b.entries[b.pos%maxEntries] = s
	b.pos++
	if b.count < maxEntries {
		b.count++
	}
	b.mu.Unlock()
}

func (b *Buffer) Recent() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.count == 0 {
		return "no recent warnings or errors"
	}
	var buf bytes.Buffer
	start := b.pos - b.count
	for i := start; i < b.pos; i++ {
		buf.WriteString(b.entries[i%maxEntries])
		buf.WriteByte('\n')
	}
	return strings.TrimRight(buf.String(), "\n")
}

type Handler struct {
	buf   *Buffer
	inner slog.Handler
}

func New(inner slog.Handler) (*Handler, *Buffer) {
	buf := &Buffer{}
	return &Handler{buf: buf, inner: inner}, buf
}

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		var sb strings.Builder
		fmt.Fprintf(&sb, "%s %s %s", r.Time.Format("15:04:05"), r.Level, r.Message)
		r.Attrs(func(a slog.Attr) bool {
			fmt.Fprintf(&sb, " %s=%v", a.Key, a.Value)
			return true
		})
		h.buf.add(sb.String())
	}
	return h.inner.Handle(ctx, r)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{buf: h.buf, inner: h.inner.WithAttrs(attrs)}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{buf: h.buf, inner: h.inner.WithGroup(name)}
}
