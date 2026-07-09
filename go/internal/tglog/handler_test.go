package tglog

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestFormatIncludesMessageAndAttrs(t *testing.T) {
	t.Parallel()

	record := slog.NewRecord(time.Date(2026, 7, 9, 7, 0, 0, 0, time.UTC), slog.LevelError, "ошибка подключения", 0)
	msg := formatAlert("AntiSpamBotTG", "Ошибка в работе бота", []string{record.Message})

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

	event := classifyErrorEvent(
		"не удалось подключиться к PostgreSQL, глобальное обучение отключено",
		map[string]string{
			"error": `PostgreSQL недоступен после 6 попыток: failed SASL auth: FATAL: password authentication failed for user "bad"`,
		},
	)
	if event == nil {
		t.Fatal("classifyErrorEvent() returned nil")
	}
	msg := formatAlert("AntiSpamBotTG", event.title, event.details)

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

func TestClassifyRecoveryEvent(t *testing.T) {
	t.Parallel()

	event := classifyRecoveryEvent("подключение к PostgreSQL установлено, глобальное обучение включено")
	if event == nil {
		t.Fatal("classifyRecoveryEvent() returned nil")
	}
	if event.key != "database" {
		t.Fatalf("event.key = %q, want database", event.key)
	}
	if !strings.Contains(formatResolved("AntiSpamBotTG", event.title, event.details), "Подключение к PostgreSQL восстановлено.") {
		t.Fatal("resolved message does not contain recovery text")
	}
}
