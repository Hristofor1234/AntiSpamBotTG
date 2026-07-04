package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	tgbot "github.com/go-telegram/bot"
)

// maxRetryAttempts — сколько раз повторяем вызов Telegram API при ответе
// 429 Too Many Requests, прежде чем сдаться и вернуть ошибку вызывающему.
const maxRetryAttempts = 3

// withRetry выполняет fn и, если Telegram отвечает 429 Too Many Requests,
// ждёт ровно столько секунд, сколько попросил Telegram (RetryAfter), и
// повторяет попытку — вместо того чтобы просто залогировать ошибку и
// потерять действие (например, недо-бан флудера). Любая другая ошибка
// возвращается сразу, без повторов.
func withRetry(ctx context.Context, logger *slog.Logger, action string, fn func() error) error {
	var lastErr error

	for attempt := 1; attempt <= maxRetryAttempts; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err

		var rateLimitErr *tgbot.TooManyRequestsError
		if !errors.As(err, &rateLimitErr) {
			// Не rate-limit — повторять бессмысленно, отдаём ошибку как есть.
			return err
		}

		wait := time.Duration(rateLimitErr.RetryAfter) * time.Second
		logger.Warn("Telegram API ответил 429, ждём и повторяем",
			"action", action, "attempt", attempt, "retry_after", wait)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}

	return fmt.Errorf("%s: превышено число повторов после 429 Too Many Requests: %w", action, lastErr)
}
