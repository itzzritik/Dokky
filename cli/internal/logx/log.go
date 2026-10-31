package logx

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/obscure"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	mask        = "***"
	minPlainLen = 4
)

var (
	mu      sync.RWMutex
	values  []string
	secrets map[string]struct{}
	plain   []string

	obscuredRun = regexp.MustCompile(`[A-Za-z0-9_-]{22,}`)
)

func New(path string) *slog.Logger {
	w := &lumberjack.Logger{Filename: path, MaxSize: 10, MaxBackups: 3}
	h := &redactHandler{inner: slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})}

	fs.GetConfig(context.Background()).LogLevel = fs.LogLevelNotice
	fs.SetLogger(&noticeHandler{h})
	l := slog.New(h)
	slog.SetDefault(l)
	return l
}

func SetSecrets(vs []string) {
	set, long := index(vs)
	mu.Lock()
	values, secrets, plain = slices.Clone(vs), set, long
	mu.Unlock()
}

func index(vs []string) (map[string]struct{}, []string) {
	set := make(map[string]struct{}, len(vs))
	var long []string
	for _, v := range vs {
		if v == "" {
			continue
		}
		set[v] = struct{}{}
		if len(v) >= minPlainLen {
			long = append(long, v)
		}
	}
	// longest first so a secret containing another is masked whole
	sort.Slice(long, func(i, j int) bool { return len(long[i]) > len(long[j]) })
	return set, long
}

func Redact(s string, extra ...string) string {
	mu.RLock()
	set, long := secrets, plain
	if len(extra) > 0 {
		set, long = index(append(slices.Clone(values), extra...))
	}
	mu.RUnlock()
	if len(set) == 0 {
		return s
	}
	s = obscuredRun.ReplaceAllStringFunc(s, func(run string) string {
		if v, err := obscure.Reveal(run); err == nil {
			if _, ok := set[v]; ok {
				return mask
			}
		}
		return run
	})
	for _, v := range long {
		s = strings.ReplaceAll(s, v, mask)
	}
	return s
}

func redactAttr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, Redact(v.String()))
	case slog.KindGroup:
		g := v.Group()
		out := make([]any, len(g))
		for i, ga := range g {
			out[i] = redactAttr(ga)
		}
		return slog.Group(a.Key, out...)
	case slog.KindAny:
		switch x := v.Any().(type) {
		case error:
			return slog.String(a.Key, Redact(x.Error()))
		case interface{ String() string }:
			return slog.String(a.Key, Redact(x.String()))
		default:
			return slog.String(a.Key, Redact(fmt.Sprintf("%+v", x)))
		}
	}
	return a
}

type redactHandler struct{ inner slog.Handler }

func (h *redactHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *redactHandler) Handle(ctx context.Context, r slog.Record) error {
	nr := slog.NewRecord(r.Time, r.Level, Redact(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		nr.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, nr)
}

func (h *redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = redactAttr(a)
	}
	return &redactHandler{inner: h.inner.WithAttrs(out)}
}

func (h *redactHandler) WithGroup(name string) slog.Handler {
	return &redactHandler{inner: h.inner.WithGroup(name)}
}

type noticeHandler struct{ slog.Handler }

func (h *noticeHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= fs.SlogLevelNotice && h.Handler.Enabled(ctx, l)
}

func (h *noticeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &noticeHandler{h.Handler.WithAttrs(attrs)}
}

func (h *noticeHandler) WithGroup(name string) slog.Handler {
	return &noticeHandler{h.Handler.WithGroup(name)}
}
