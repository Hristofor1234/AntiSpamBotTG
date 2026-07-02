// Package handler содержит обработчики Telegram-обновлений: команды
// администратора и фильтр спам-сообщений.
package handler

import (
	"context"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Hristofor1234/AntiSpamBotTG/internal/database"
)

const helpText = "/add <слова> — добавить\n" +
	"/remove <слова> — удалить\n" +
	"/list — показать список\n" +
	"/commands — команды"

// Commands объединяет обработчики административных команд бота.
type Commands struct {
	db     *database.DB
	logger *slog.Logger
}

// NewCommands создаёт набор обработчиков команд.
func NewCommands(db *database.DB, logger *slog.Logger) *Commands {
	return &Commands{db: db, logger: logger}
}

// commandArgs возвращает аргументы команды — все слова сообщения, кроме
// самой команды (первого токена).
func commandArgs(update *models.Update) []string {
	if update.Message == nil {
		return nil
	}
	parts := strings.Fields(update.Message.Text)
	if len(parts) <= 1 {
		return nil
	}
	return parts[1:]
}

func reply(ctx context.Context, b *bot.Bot, update *models.Update, text string) {
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   text,
	})
}

// Start обрабатывает команду /start. Доступна всем пользователям.
func (c *Commands) Start(ctx context.Context, b *bot.Bot, update *models.Update) {
	reply(ctx, b, update, "Антиспам-бот активен. Используйте /commands для списка команд.")
}

// Help обрабатывает команду /commands — выводит список доступных команд.
func (c *Commands) Help(ctx context.Context, b *bot.Bot, update *models.Update) {
	reply(ctx, b, update, helpText)
}

// List обрабатывает команду /list — выводит все слова из чёрного списка.
func (c *Commands) List(ctx context.Context, b *bot.Bot, update *models.Update) {
	words := c.db.ListWords()
	if len(words) == 0 {
		reply(ctx, b, update, "Список пуст.")
		return
	}
	reply(ctx, b, update, "Слова:\n"+strings.Join(words, ", "))
}

// Add обрабатывает команду /add — добавляет слова в чёрный список.
func (c *Commands) Add(ctx context.Context, b *bot.Bot, update *models.Update) {
	words := commandArgs(update)
	if len(words) == 0 {
		reply(ctx, b, update, "Нечего добавлять.")
		return
	}

	added, err := c.db.AddWords(ctx, words)
	if err != nil {
		c.logger.Error("ошибка добавления слов", "error", err)
		reply(ctx, b, update, "Произошла ошибка при добавлении слов.")
		return
	}

	if len(added) == 0 {
		reply(ctx, b, update, "Нечего добавлять.")
		return
	}

	c.logger.Info("админ добавил слова",
		"username", usernameOf(update), "words", added)
	reply(ctx, b, update, "Добавлены: "+strings.Join(added, ", "))
}

// Remove обрабатывает команду /remove — удаляет слова из чёрного списка.
func (c *Commands) Remove(ctx context.Context, b *bot.Bot, update *models.Update) {
	words := commandArgs(update)
	if len(words) == 0 {
		reply(ctx, b, update, "Нечего удалять.")
		return
	}

	removed, err := c.db.RemoveWords(ctx, words)
	if err != nil {
		c.logger.Error("ошибка удаления слов", "error", err)
		reply(ctx, b, update, "Произошла ошибка при удалении слов.")
		return
	}

	if len(removed) == 0 {
		reply(ctx, b, update, "Нечего удалять.")
		return
	}

	c.logger.Info("админ удалил слова",
		"username", usernameOf(update), "words", removed)
	reply(ctx, b, update, "Удалены: "+strings.Join(removed, ", "))
}

func usernameOf(update *models.Update) string {
	if update.Message == nil || update.Message.From == nil {
		return ""
	}
	return update.Message.From.Username
}
