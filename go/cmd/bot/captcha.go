package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Hristofor1234/AntiSpamBotTG/internal/captcha"
	"github.com/Hristofor1234/AntiSpamBotTG/internal/config"
)

// captchaCallbackPrefix — префикс callback_data кнопки капчи, формат
// "captcha:<chatID>:<userID>".
const captchaCallbackPrefix = "captcha:"

// handleNewChatMembers мутит только что вступивших участников и отправляет
// каждому приглашение подтвердить, что он не бот. Ботов не капчуем.
func handleNewChatMembers(ctx context.Context, b *tgbot.Bot, cm *captcha.Manager, cfg *config.Config, logger *slog.Logger, chatID int64, members []models.User) {
	for _, member := range members {
		if member.IsBot {
			continue
		}
		handleNewChatMember(ctx, b, cm, cfg, logger, chatID, member)
	}
}

func handleNewChatMember(ctx context.Context, b *tgbot.Bot, cm *captcha.Manager, cfg *config.Config, logger *slog.Logger, chatID int64, member models.User) {
	if _, err := b.RestrictChatMember(ctx, &tgbot.RestrictChatMemberParams{
		ChatID:      chatID,
		UserID:      member.ID,
		Permissions: &models.ChatPermissions{}, // все поля false/omitempty => Telegram трактует как полный мьют
	}); err != nil {
		logger.Error("не удалось ограничить нового участника перед капчей",
			"error", err, "chat_id", chatID, "user_id", member.ID)
		return
	}

	name := member.FirstName
	if name == "" {
		name = fmt.Sprintf("id%d", member.ID)
	}

	msg, err := b.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: chatID,
		Text: fmt.Sprintf(
			"Добро пожаловать, %s! Подтвердите, что вы не бот, нажав кнопку ниже — иначе через %s будете удалены из чата.",
			name, cfg.CaptchaTimeout,
		),
		ReplyMarkup: &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{{Text: "✅ Я не бот", CallbackData: fmt.Sprintf("%s%d:%d", captchaCallbackPrefix, chatID, member.ID)}},
			},
		},
	})
	if err != nil {
		logger.Error("не удалось отправить капчу", "error", err, "chat_id", chatID, "user_id", member.ID)
		return
	}

	cm.Track(chatID, member.ID, msg.ID, func() {
		logger.Warn("капча не пройдена вовремя, участник удалён из чата",
			"chat_id", chatID, "user_id", member.ID)

		if _, err := b.BanChatMember(ctx, &tgbot.BanChatMemberParams{ChatID: chatID, UserID: member.ID}); err != nil {
			logger.Error("не удалось удалить участника по таймауту капчи",
				"error", err, "chat_id", chatID, "user_id", member.ID)
			return
		}
		// Сразу снимаем бан: цель — выставить за дверь и дать шанс зайти
		// заново и пройти капчу, а не заблокировать навсегда.
		if _, err := b.UnbanChatMember(ctx, &tgbot.UnbanChatMemberParams{
			ChatID: chatID, UserID: member.ID, OnlyIfBanned: true,
		}); err != nil {
			logger.Error("не удалось снять технический бан после кика по капче",
				"error", err, "chat_id", chatID, "user_id", member.ID)
		}
		if _, err := b.DeleteMessage(ctx, &tgbot.DeleteMessageParams{ChatID: chatID, MessageID: msg.ID}); err != nil {
			logger.Error("не удалось удалить сообщение с капчей после таймаута",
				"error", err, "chat_id", chatID)
		}
	})
}

// registerCaptchaCallbackHandler регистрирует обработку нажатия кнопки
// "Я не бот".
func registerCaptchaCallbackHandler(b *tgbot.Bot, cm *captcha.Manager, logger *slog.Logger) {
	b.RegisterHandler(tgbot.HandlerTypeCallbackQueryData, captchaCallbackPrefix, tgbot.MatchTypePrefix,
		func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
			cq := update.CallbackQuery
			if cq == nil {
				return
			}

			chatID, userID, ok := parseCaptchaCallbackData(cq.Data)
			if !ok {
				return
			}

			answer := &tgbot.AnswerCallbackQueryParams{CallbackQueryID: cq.ID}

			// Кнопка предназначена только тому, кого капчуют — иначе кто
			// угодно в чате мог бы за него подтвердить.
			if cq.From.ID != userID {
				answer.Text = "Эта кнопка не для вас."
				answer.ShowAlert = true
				_, _ = b.AnswerCallbackQuery(ctx, answer)
				return
			}

			challengeMessageID, wasPending := cm.Resolve(chatID, userID)
			if !wasPending {
				answer.Text = "Капча уже обработана."
				_, _ = b.AnswerCallbackQuery(ctx, answer)
				return
			}

			if err := unmuteChatMember(ctx, b, chatID, userID); err != nil {
				logger.Error("не удалось снять ограничения после капчи",
					"error", err, "chat_id", chatID, "user_id", userID)
			}
			if _, err := b.DeleteMessage(ctx, &tgbot.DeleteMessageParams{ChatID: chatID, MessageID: challengeMessageID}); err != nil {
				logger.Error("не удалось удалить сообщение с капчей", "error", err, "chat_id", chatID)
			}

			answer.Text = "Спасибо, подтверждено!"
			if _, err := b.AnswerCallbackQuery(ctx, answer); err != nil {
				logger.Error("не удалось ответить на callback капчи", "error", err)
			}
		},
	)
}

// parseCaptchaCallbackData разбирает "captcha:<chatID>:<userID>".
func parseCaptchaCallbackData(data string) (chatID, userID int64, ok bool) {
	rest := strings.TrimPrefix(data, captchaCallbackPrefix)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}

	chatID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	userID, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return chatID, userID, true
}

// unmuteChatMember возвращает пользователю права по умолчанию для этого
// чата (запрашивает их через getChat), а не жёстко захардкоженный набор
// "всё разрешено" — так ограничение снимается ровно до текущих настроек
// чата, даже если админы что-то в них меняли (например, отключили опросы).
func unmuteChatMember(ctx context.Context, b *tgbot.Bot, chatID, userID int64) error {
	chat, err := b.GetChat(ctx, &tgbot.GetChatParams{ChatID: chatID})
	if err != nil {
		return fmt.Errorf("getChat: %w", err)
	}

	permissions := chat.Permissions
	if permissions == nil {
		// Telegram не вернул текущие права чата — разумный дефолт "без ограничений".
		permissions = &models.ChatPermissions{
			CanSendMessages:       true,
			CanSendAudios:         true,
			CanSendDocuments:      true,
			CanSendPhotos:         true,
			CanSendVideos:         true,
			CanSendVideoNotes:     true,
			CanSendVoiceNotes:     true,
			CanSendPolls:          true,
			CanSendOtherMessages:  true,
			CanAddWebPagePreviews: true,
			CanInviteUsers:        true,
		}
	}

	_, err = b.RestrictChatMember(ctx, &tgbot.RestrictChatMemberParams{
		ChatID:      chatID,
		UserID:      userID,
		Permissions: permissions,
	})
	return err
}
