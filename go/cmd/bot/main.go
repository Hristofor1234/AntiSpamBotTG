// Команда bot — точка входа антиспам-бота для Telegram.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	tgbot "github.com/go-telegram/bot"
	"github.com/joho/godotenv"

	"github.com/Hristofor1234/AntiSpamBotTG/internal/config"
	"github.com/Hristofor1234/AntiSpamBotTG/internal/database"
	"github.com/Hristofor1234/AntiSpamBotTG/internal/handler"
	"github.com/Hristofor1234/AntiSpamBotTG/internal/middleware"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// .env нужен только для локального запуска без Docker; в контейнере
	// переменные приходят из окружения, и файла может не быть — это не ошибка.
	_ = godotenv.Load()

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		logger.Error("ошибка конфигурации", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	db, err := database.New(ctx, cfg.DSN())
	if err != nil {
		logger.Error("ошибка подключения к БД", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.InitSchema(ctx); err != nil {
		logger.Error("ошибка инициализации схемы БД", "error", err)
		os.Exit(1)
	}
	logger.Info("схема БД инициализирована")

	n, err := db.LoadCache(ctx)
	if err != nil {
		logger.Error("ошибка загрузки кэша ключевых слов", "error", err)
		os.Exit(1)
	}
	logger.Info("кэш ключевых слов загружен", "count", n)

	cmds := handler.NewCommands(db, logger)
	spam := handler.NewSpam(db, logger)
	requireAccess := middleware.RequireAccess(cfg, logger)

	b, err := tgbot.New(cfg.BotToken, tgbot.WithDefaultHandler(spam.Filter))
	if err != nil {
		logger.Error("ошибка инициализации бота", "error", err)
		os.Exit(1)
	}

	// /start доступен всем пользователям.
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "start", tgbot.MatchTypeCommand, cmds.Start)

	// Команды управления чёрным списком — только для ALLOWED_USERS.
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "commands", tgbot.MatchTypeCommand, cmds.Help, requireAccess)
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "add", tgbot.MatchTypeCommand, cmds.Add, requireAccess)
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "remove", tgbot.MatchTypeCommand, cmds.Remove, requireAccess)
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "list", tgbot.MatchTypeCommand, cmds.List, requireAccess)

	logger.Info("бот запущен")
	b.Start(ctx)
	logger.Info("бот остановлен")
}
