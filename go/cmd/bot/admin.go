package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Hristofor1234/AntiSpamBotTG/internal/storage"
)

// isChatAdmin проверяет, является ли userID администратором или создателем
// чата chatID — /addspam и /removespam должны быть доступны только
// модераторам, а не любому участнику чата.
func isChatAdmin(ctx context.Context, b *tgbot.Bot, chatID, userID int64) (bool, error) {
	member, err := b.GetChatMember(ctx, &tgbot.GetChatMemberParams{ChatID: chatID, UserID: userID})
	if err != nil {
		return false, err
	}
	return member.Type == models.ChatMemberTypeOwner || member.Type == models.ChatMemberTypeAdministrator, nil
}

// commandArgument возвращает текст команды без самой команды: например,
// для "/addspam казино" при паттерне "addspam" вернёт "казино". Ищет
// entity бота-команды в сообщении, поэтому корректно работает и с
// "/addspam@BotUsername казино".
func commandArgument(msg *models.Message) string {
	for _, e := range msg.Entities {
		if e.Type == models.MessageEntityTypeBotCommand && e.Offset+e.Length <= len(msg.Text) {
			return strings.TrimSpace(msg.Text[e.Offset+e.Length:])
		}
	}
	return ""
}

// registerAdminHandlers регистрирует команды управления фильтром по словам:
// /addspam, /removespam, /triggers. Требуют подключённой PostgreSQL — без
// неё отвечают понятным сообщением, а не молчат.
func registerAdminHandlers(b *tgbot.Bot, store *storage.Store, logger *slog.Logger) {
	reply := func(ctx context.Context, msg *models.Message, text string) {
		if _, err := b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID:          msg.Chat.ID,
			Text:            text,
			ReplyParameters: &models.ReplyParameters{MessageID: msg.ID},
		}); err != nil {
			logger.Error("не удалось ответить на admin-команду", "error", err, "chat_id", msg.Chat.ID)
		}
	}

	// requireAdmin возвращает true, если можно продолжать обработку команды
	// (отправитель — админ чата); иначе сама отвечает пользователю и
	// возвращает false.
	requireAdmin := func(ctx context.Context, msg *models.Message) bool {
		if msg.From == nil {
			return false
		}
		admin, err := isChatAdmin(ctx, b, msg.Chat.ID, msg.From.ID)
		if err != nil {
			logger.Error("не удалось проверить права администратора",
				"error", err, "chat_id", msg.Chat.ID, "user_id", msg.From.ID)
			reply(ctx, msg, "Не удалось проверить права администратора, попробуйте позже.")
			return false
		}
		if !admin {
			reply(ctx, msg, "Эта команда доступна только администраторам чата.")
			return false
		}
		return true
	}

	b.RegisterHandler(tgbot.HandlerTypeMessageText, "addspam", tgbot.MatchTypeCommand,
		func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
			msg := update.Message
			if msg == nil {
				return
			}
			if store == nil {
				reply(ctx, msg, "Фильтр по словам недоступен: не подключена PostgreSQL (DATABASE_URL).")
				return
			}
			if !requireAdmin(ctx, msg) {
				return
			}

			phrase := commandArgument(msg)
			if phrase == "" {
				reply(ctx, msg, "Использование: /addspam <слово или фраза>")
				return
			}

			if err := store.AddTrigger(ctx, msg.Chat.ID, phrase); err != nil {
				logger.Error("не удалось добавить триггер", "error", err, "chat_id", msg.Chat.ID)
				reply(ctx, msg, "Не удалось сохранить фразу, попробуйте позже.")
				return
			}
			reply(ctx, msg, fmt.Sprintf("Добавлено в фильтр: «%s»", phrase))
		},
	)

	b.RegisterHandler(tgbot.HandlerTypeMessageText, "removespam", tgbot.MatchTypeCommand,
		func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
			msg := update.Message
			if msg == nil {
				return
			}
			if store == nil {
				reply(ctx, msg, "Фильтр по словам недоступен: не подключена PostgreSQL (DATABASE_URL).")
				return
			}
			if !requireAdmin(ctx, msg) {
				return
			}

			phrase := commandArgument(msg)
			if phrase == "" {
				reply(ctx, msg, "Использование: /removespam <слово или фраза>")
				return
			}

			removed, err := store.RemoveTrigger(ctx, msg.Chat.ID, phrase)
			if err != nil {
				logger.Error("не удалось удалить триггер", "error", err, "chat_id", msg.Chat.ID)
				reply(ctx, msg, "Не удалось удалить фразу, попробуйте позже.")
				return
			}
			if !removed {
				reply(ctx, msg, fmt.Sprintf("«%s» и не было в фильтре.", phrase))
				return
			}
			reply(ctx, msg, fmt.Sprintf("Удалено из фильтра: «%s»", phrase))
		},
	)

	b.RegisterHandler(tgbot.HandlerTypeMessageText, "triggers", tgbot.MatchTypeCommand,
		func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
			msg := update.Message
			if msg == nil {
				return
			}
			if store == nil {
				reply(ctx, msg, "Фильтр по словам недоступен: не подключена PostgreSQL (DATABASE_URL).")
				return
			}
			if !requireAdmin(ctx, msg) {
				return
			}

			phrases, err := store.ListTriggers(ctx, msg.Chat.ID)
			if err != nil {
				logger.Error("не удалось получить список триггеров", "error", err, "chat_id", msg.Chat.ID)
				reply(ctx, msg, "Не удалось получить список, попробуйте позже.")
				return
			}
			if len(phrases) == 0 {
				reply(ctx, msg, "Фильтр по словам для этого чата пуст.")
				return
			}
			reply(ctx, msg, "Триггер-фразы этого чата:\n— "+strings.Join(phrases, "\n— "))
		},
	)
}
