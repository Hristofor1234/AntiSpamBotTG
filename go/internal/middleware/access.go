// Package middleware содержит промежуточные обработчики (ACL и т.п.),
// которые оборачивают обработчики команд бота.
package middleware

import (
	"context"
	"log/slog"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// AccessChecker проверяет, разрешён ли доступ пользователю с данным username.
type AccessChecker interface {
	IsAllowed(username string) bool
}

// RequireAccess возвращает middleware, который пропускает вызов дальше
// только если отправитель сообщения есть в списке ALLOWED_USERS.
// Иначе отвечает "⛔ У вас нет доступа." и не выполняет команду.
func RequireAccess(checker AccessChecker, logger *slog.Logger) func(next bot.HandlerFunc) bot.HandlerFunc {
	return func(next bot.HandlerFunc) bot.HandlerFunc {
		return func(ctx context.Context, b *bot.Bot, update *models.Update) {
			if update.Message == nil || update.Message.From == nil {
				return
			}

			username := update.Message.From.Username
			if !checker.IsAllowed(username) {
				logger.Warn("отклонён запрос: нет доступа", "username", username)
				_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID: update.Message.Chat.ID,
					Text:   "⛔ У вас нет доступа.",
				})
				return
			}

			next(ctx, b, update)
		}
	}
}
