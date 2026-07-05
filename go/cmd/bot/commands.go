package main

import (
	"context"
	"log/slog"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

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
