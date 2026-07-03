// Команда bot — точка входа антифлуд-бота для Telegram.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"

	"github.com/Hristofor1234/AntiSpamBotTG/internal/config"
	"github.com/Hristofor1234/AntiSpamBotTG/internal/dispatcher"
	"github.com/Hristofor1234/AntiSpamBotTG/internal/ratelimit"
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

	limiter := ratelimit.New(cfg.RateLimitCount, cfg.RateLimitWindow)
	go limiter.RunCleanup(ctx, time.Minute)

	// Диспетчеру для вызова Telegram API нужна ссылка на *bot.Bot, а
	// хендлеру бота — ссылка на диспетчер. Разрываем цикл: хендлер
	// захватывает указатель d по замыканию, значение появится в нём до
	// того, как реально начнут приходить обновления (b.Start ниже).
	var d *dispatcher.Dispatcher

	defaultHandler := func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
		if update.Message == nil || update.Message.From == nil {
			return
		}
		from := update.Message.From
		if from.IsBot {
			return
		}

		d.Submit(dispatcher.Update{
			MessageID: update.Message.ID,
			ChatID:    update.Message.Chat.ID,
			UserID:    from.ID,
			Username:  from.Username,
			Text:      update.Message.Text,
			Timestamp: time.Unix(int64(update.Message.Date), 0),
		})
	}

	b, err := tgbot.New(cfg.BotToken, tgbot.WithDefaultHandler(defaultHandler))
	if err != nil {
		logger.Error("ошибка инициализации бота", "error", err)
		os.Exit(1)
	}

	b.RegisterHandler(tgbot.HandlerTypeMessageText, "start", tgbot.MatchTypeCommand,
		func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
			_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text: fmt.Sprintf(
					"Антифлуд-бот активен. Лимит: %d сообщ. / %s — при превышении сообщение удаляется, автор банится с отзывом недавних сообщений.",
					cfg.RateLimitCount, cfg.RateLimitWindow,
				),
			})
		},
	)

	d = dispatcher.New(cfg.WorkerCount, cfg.QueueSize, limiter, b, logger)
	d.Start(ctx)

	logger.Info("бот запущен",
		"rate_limit_count", cfg.RateLimitCount,
		"rate_limit_window", cfg.RateLimitWindow,
		"workers", cfg.WorkerCount,
		"queue_size", cfg.QueueSize)

	b.Start(ctx)

	d.Wait()
	logger.Info("бот остановлен")
}
