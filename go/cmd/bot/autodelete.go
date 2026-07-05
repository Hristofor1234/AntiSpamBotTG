package main

import (
	"context"
	"log/slog"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// deleteRequestTimeout — таймаут на сами вызовы deleteMessage при
// автоудалении (не путать с autoDeleteDelay — временем ожидания перед ними).
const deleteRequestTimeout = 10 * time.Second

// scheduleAutoDelete планирует удаление сообщения команды (userMsgID) и
// ответа бота (botMsgID) через delay — команды администрирования не должны
// навсегда оставаться в истории чата. В личных чатах ничего не удаляется:
// там это обычная переписка, а не разовая команда, засоряющая групповой чат.
// delay <= 0 отключает автоудаление.
//
// Собственный фоновый контекст с таймаутом (а не тот ctx, что пришёл в
// хендлер) — тот же приём, что и в dispatcher.ban для обучения: хендлерный
// ctx отменяется при остановке бота (SIGTERM), и если это произойдёт во
// время ожидания delay, удаление с уже отменённым ctx было бы обречено.
func scheduleAutoDelete(b *tgbot.Bot, chat models.Chat, userMsgID, botMsgID int, delay time.Duration, logger *slog.Logger) {
	if delay <= 0 || chat.Type == models.ChatTypePrivate {
		return
	}

	go func() {
		time.Sleep(delay)

		deleteCtx, cancel := context.WithTimeout(context.Background(), deleteRequestTimeout)
		defer cancel()

		if _, err := b.DeleteMessage(deleteCtx, &tgbot.DeleteMessageParams{ChatID: chat.ID, MessageID: userMsgID}); err != nil {
			logger.Debug("не удалось удалить команду при автоудалении",
				"error", err, "chat_id", chat.ID, "message_id", userMsgID)
		}
		if botMsgID != 0 {
			if _, err := b.DeleteMessage(deleteCtx, &tgbot.DeleteMessageParams{ChatID: chat.ID, MessageID: botMsgID}); err != nil {
				logger.Debug("не удалось удалить ответ бота при автоудалении",
					"error", err, "chat_id", chat.ID, "message_id", botMsgID)
			}
		}
	}()
}

// sendAndScheduleDelete отправляет text в ответ (reply) на msg и, если
// autoDeleteDelay > 0 и чат не личный, планирует удаление обеих реплик
// (команды и ответа) через autoDeleteDelay — см. scheduleAutoDelete.
// parseMode может быть пустой строкой, если форматирование не нужно.
func sendAndScheduleDelete(ctx context.Context, b *tgbot.Bot, msg *models.Message, text string, parseMode models.ParseMode, autoDeleteDelay time.Duration, logger *slog.Logger) {
	params := &tgbot.SendMessageParams{
		ChatID:          msg.Chat.ID,
		Text:            text,
		ReplyParameters: &models.ReplyParameters{MessageID: msg.ID},
	}
	if parseMode != "" {
		params.ParseMode = parseMode
	}

	sent, err := b.SendMessage(ctx, params)
	if err != nil {
		logger.Error("не удалось отправить ответ", "error", err, "chat_id", msg.Chat.ID)
		return
	}

	scheduleAutoDelete(b, msg.Chat, msg.ID, sent.ID, autoDeleteDelay, logger)
}
