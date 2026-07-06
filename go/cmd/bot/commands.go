package main

import (
	"context"
	"log/slog"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// commandMatcher возвращает tgbot.MatchFunc, который матчит команду и без,
// и с явным "@ИмяБота" — например "/start" и "/start@AntiSpamBotTG_bot".
//
// Встроенный tgbot.MatchTypeCommand так не умеет: он сравнивает подстроку
// команды из entity буквально с pattern, включая "@ИмяБота", если он есть в
// тексте сообщения (см. исходник handlers.go библиотеки: сравнение идёт по
// data[e.Offset+1:e.Offset+e.Length], а не по части до "@"). Из-за этого
// команды с явно указанным именем бота — обычное дело в группах, особенно
// если ботов несколько, а Telegram сам подставляет "@ИмяБота" в
// автоподсказке — никогда не совпадали с зарегистрированными хендлерами и
// молча проваливались в defaultHandler, откуда шли в антиспам-конвейер как
// обычный текст, без всякого ответа.
func commandMatcher(command string) tgbot.MatchFunc {
	return func(update *models.Update) bool {
		if update.Message == nil {
			return false
		}
		text := update.Message.Text
		for _, e := range update.Message.Entities {
			if e.Type != models.MessageEntityTypeBotCommand {
				continue
			}
			if e.Offset < 0 || e.Length <= 0 || e.Offset+e.Length > len(text) {
				continue
			}

			cmd := text[e.Offset+1 : e.Offset+e.Length]
			if at := strings.IndexByte(cmd, '@'); at != -1 {
				cmd = cmd[:at]
			}
			if cmd == command {
				return true
			}
		}
		return false
	}
}

// registerCommand регистрирует текстовую команду command (без "/") —
// см. commandMatcher про то, почему это не tgbot.RegisterHandler с
// tgbot.MatchTypeCommand.
func registerCommand(b *tgbot.Bot, command string, handler tgbot.HandlerFunc) {
	b.RegisterHandlerMatchFunc(commandMatcher(command), handler)
}

// registerBotCommands регистрирует список команд для автодополнения в
// Telegram (подсказки при вводе "/"). Группы получают полный список — в том
// числе команды, доступные только администраторам (сама команда всё равно
// проверяет права при выполнении, см. admin.go; ограничивать видимость
// подсказки через BotCommandScopeChatAdministrators не стали, т.к. этот
// scope требует конкретный ChatID — то есть годится только на "один чат", а
// не глобально на все группы, и заодно скрыл бы /report/start/help от
// обычных участников). Личные чаты получают только /start и /help — ботом
// в личке нельзя ни забанить, ни настроить фильтр конкретного чата.
func registerBotCommands(ctx context.Context, b *tgbot.Bot, logger *slog.Logger) {
	groupCommands := []models.BotCommand{
		{Command: "start", Description: "Информация о боте"},
		{Command: "help", Description: "Полный список функций"},
		{Command: "report", Description: "Пожаловаться на reply-сообщение"},
		{Command: "ban", Description: "Бан reply-сообщения + обучение"},
		{Command: "addspam", Description: "Добавить слово-триггер"},
		{Command: "removespam", Description: "Удалить слово-триггер"},
		{Command: "triggers", Description: "Список триггеров чата"},
		{Command: "addcorewords", Description: "Добавить встроенную категорию слов"},
		{Command: "blockdomain", Description: "Заблокировать домен"},
		{Command: "unblockdomain", Description: "Разблокировать домен"},
		{Command: "domains", Description: "Список опасных доменов"},
	}

	privateCommands := []models.BotCommand{
		{Command: "start", Description: "Информация о боте"},
		{Command: "help", Description: "Полный список функций"},
	}

	if _, err := b.SetMyCommands(ctx, &tgbot.SetMyCommandsParams{
		Commands: groupCommands,
		Scope:    &models.BotCommandScopeAllGroupChats{},
	}); err != nil {
		logger.Error("не удалось зарегистрировать подсказки команд для групп", "error", err)
	}

	if _, err := b.SetMyCommands(ctx, &tgbot.SetMyCommandsParams{
		Commands: privateCommands,
		Scope:    &models.BotCommandScopeAllPrivateChats{},
	}); err != nil {
		logger.Error("не удалось зарегистрировать подсказки команд для личных чатов", "error", err)
	}
}
