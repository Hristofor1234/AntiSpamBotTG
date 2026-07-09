package tglog

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestFormatIncludesMessageAndAttrs(t *testing.T) {
	t.Parallel()

	h := &AsyncHandler{
		minLevel: slog.LevelError,
		source:   "AntiSpamBotTG",
		attrs: []slog.Attr{
			slog.String("service", "bot"),
		},
	}

	record := slog.NewRecord(time.Date(2026, 7, 9, 7, 0, 0, 0, time.UTC), slog.LevelError, "ошибка подключения", 0)
	record.AddAttrs(
		slog.Int64("chat_id", -100123),
		slog.Group("error",
			slog.String("kind", "telegram"),
			slog.String("detail", "timeout"),
		),
	)

	msg := h.format(record)

	for _, want := range []string{
		"ERROR LOG: AntiSpamBotTG",
		"level=ERROR",
		"message=ошибка подключения",
		"service=bot",
		"chat_id=-100123",
		"error.kind=telegram",
		"error.detail=timeout",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("formatted message %q does not contain %q", msg, want)
		}
	}
}
