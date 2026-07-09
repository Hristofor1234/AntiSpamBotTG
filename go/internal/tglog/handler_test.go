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
		"❌ AntiSpamBotTG | Ошибка в работе бота",
		"ошибка подключения",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("formatted message %q does not contain %q", msg, want)
		}
	}
}

func TestFormatHumanizesDatabaseError(t *testing.T) {
	t.Parallel()

	h := &AsyncHandler{
		minLevel: slog.LevelError,
		source:   "AntiSpamBotTG",
	}

	record := slog.NewRecord(time.Date(2026, 7, 9, 13, 0, 0, 0, time.UTC), slog.LevelError, "не удалось подключиться к PostgreSQL, глобальное обучение отключено", 0)
	record.AddAttrs(slog.String("error", `PostgreSQL недоступен после 6 попыток: failed SASL auth: FATAL: password authentication failed for user "bad"`))

	msg := h.format(record)

	for _, want := range []string{
		"❌ AntiSpamBotTG | Проблема с базой данных",
		"Бот не смог подключиться к PostgreSQL.",
		"Причина: неверный логин или пароль к базе данных.",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("formatted message %q does not contain %q", msg, want)
		}
	}
}
