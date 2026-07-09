package tglog

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
)

const (
	maxMessageLen = 4096
	sendTimeout   = 5 * time.Second
)

// AsyncHandler дублирует slog-записи в Telegram без блокировки основного потока.
type AsyncHandler struct {
	minLevel slog.Leveler
	source   string
	notifier *notifier
	attrs    []slog.Attr
	groups   []string
}

type notifier struct {
	bot      *tgbot.Bot
	chatID   int64
	threadID int
	queue    chan string
	wg       sync.WaitGroup
}

// NewAsyncHandler создаёт slog.Handler, который отправляет записи указанного
// уровня и выше в Telegram-чат/тему.
func NewAsyncHandler(botToken string, chatID int64, threadID int, source string, minLevel slog.Leveler) (*AsyncHandler, func(), error) {
	b, err := tgbot.New(botToken)
	if err != nil {
		return nil, nil, fmt.Errorf("create telegram log bot: %w", err)
	}

	n := &notifier{
		bot:      b,
		chatID:   chatID,
		threadID: threadID,
		queue:    make(chan string, 100),
	}
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		for msg := range n.queue {
			ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
			_, err := n.bot.SendMessage(ctx, &tgbot.SendMessageParams{
				ChatID:          n.chatID,
				MessageThreadID: n.threadID,
				Text:            msg,
			})
			cancel()
			if err != nil {
				fmt.Fprintf(os.Stderr, "tglog send failed: chat_id=%d thread_id=%d error=%v\n", n.chatID, n.threadID, err)
			}
		}
	}()

	h := &AsyncHandler{
		minLevel: minLevel,
		source:   source,
		notifier: n,
	}

	closeFn := func() {
		close(n.queue)
		n.wg.Wait()
	}
	return h, closeFn, nil
}

func (h *AsyncHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel.Level()
}

func (h *AsyncHandler) Handle(_ context.Context, r slog.Record) error {
	msg := h.format(r)
	select {
	case h.notifier.queue <- msg:
	default:
	}
	return nil
}

func (h *AsyncHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cloned := &AsyncHandler{
		minLevel: h.minLevel,
		source:   h.source,
		notifier: h.notifier,
		attrs:    append(append([]slog.Attr(nil), h.attrs...), attrs...),
		groups:   append([]string(nil), h.groups...),
	}
	return cloned
}

func (h *AsyncHandler) WithGroup(name string) slog.Handler {
	cloned := &AsyncHandler{
		minLevel: h.minLevel,
		source:   h.source,
		notifier: h.notifier,
		attrs:    append([]slog.Attr(nil), h.attrs...),
		groups:   append(append([]string(nil), h.groups...), name),
	}
	return cloned
}

func (h *AsyncHandler) format(r slog.Record) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("ERROR LOG: %s", h.source))
	lines = append(lines, fmt.Sprintf("time=%s", r.Time.UTC().Format(time.RFC3339)))
	lines = append(lines, fmt.Sprintf("level=%s", r.Level.String()))
	lines = append(lines, fmt.Sprintf("message=%s", r.Message))

	for _, attr := range h.attrs {
		lines = appendAttr(lines, h.groups, attr)
	}
	r.Attrs(func(attr slog.Attr) bool {
		lines = appendAttr(lines, h.groups, attr)
		return true
	})

	msg := strings.Join(lines, "\n")
	if len(msg) <= maxMessageLen {
		return msg
	}
	return msg[:maxMessageLen-14] + "\n...truncated"
}

func appendAttr(lines []string, groups []string, attr slog.Attr) []string {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return lines
	}

	key := attr.Key
	if len(groups) > 0 && key != "" {
		key = strings.Join(append(append([]string(nil), groups...), key), ".")
	}

	if attr.Value.Kind() == slog.KindGroup {
		nextGroups := append([]string(nil), groups...)
		if attr.Key != "" {
			nextGroups = append(nextGroups, attr.Key)
		}
		for _, nested := range attr.Value.Group() {
			lines = appendAttr(lines, nextGroups, nested)
		}
		return lines
	}

	lines = append(lines, fmt.Sprintf("%s=%s", key, valueString(attr.Value)))
	return lines
}

func valueString(v slog.Value) string {
	v = v.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindBool:
		if v.Bool() {
			return "true"
		}
		return "false"
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindFloat64:
		return fmt.Sprintf("%g", v.Float64())
	case slog.KindInt64:
		return fmt.Sprintf("%d", v.Int64())
	case slog.KindTime:
		return v.Time().UTC().Format(time.RFC3339)
	case slog.KindUint64:
		return fmt.Sprintf("%d", v.Uint64())
	default:
		return fmt.Sprint(v.Any())
	}
}
