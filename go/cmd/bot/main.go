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
	"github.com/Hristofor1234/AntiSpamBotTG/internal/storage"
)

// dbInitTimeout — сколько ждём подключения к PostgreSQL при старте
// (storage.New сам делает несколько попыток Ping с паузами — таймаут должен
// с запасом их покрывать).
const dbInitTimeout = 30 * time.Second

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

	// Глобальное обучение (PostgreSQL) — необязательная фича. Если
	// DATABASE_URL не задан или БД оказалась недоступна при старте, бот не
	// падает — просто работает без глобального чёрного списка, только на
	// rate-limit, как раньше.
	var store *storage.Store
	if cfg.DatabaseURL != "" {
		initCtx, initCancel := context.WithTimeout(ctx, dbInitTimeout)
		s, err := storage.New(initCtx, cfg.DatabaseURL)
		initCancel()

		if err != nil {
			logger.Error("не удалось подключиться к PostgreSQL, глобальное обучение отключено",
				"error", err)
		} else {
			store = s
			defer store.Close()
			logger.Info("подключение к PostgreSQL установлено, глобальное обучение включено")
		}
	} else {
		logger.Info("DATABASE_URL не задан, глобальное обучение отключено (только rate-limit)")
	}

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

	opts := []tgbot.Option{tgbot.WithDefaultHandler(defaultHandler)}
	if cfg.WebhookSecretToken != "" {
		// Telegram присылает этот токен в заголовке X-Telegram-Bot-Api-Secret-Token
		// с каждым webhook-запросом — библиотека сверяет его сама и молча
		// игнорирует запрос при несовпадении. Это защита от поддельных
		// POST-запросов на публичный /webhook от кого угодно в интернете.
		opts = append(opts, tgbot.WithWebhookSecretToken(cfg.WebhookSecretToken))
	}

	b, err := tgbot.New(cfg.BotToken, opts...)
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

	d = dispatcher.New(cfg.WorkerCount, cfg.QueueSize, limiter, b, store, logger)
	d.Start(ctx)

	mode := "long-polling"
	if cfg.WebhookURL != "" {
		mode = "webhook"
	}

	logger.Info("бот запущен",
		"mode", mode,
		"rate_limit_count", cfg.RateLimitCount,
		"rate_limit_window", cfg.RateLimitWindow,
		"workers", cfg.WorkerCount,
		"queue_size", cfg.QueueSize)

	if cfg.WebhookURL != "" {
		if err := runWebhook(ctx, b, cfg, logger); err != nil {
			logger.Error("webhook-сервер завершился с ошибкой", "error", err)
		}
	} else {
		b.Start(ctx)
	}

	d.Wait()
	logger.Info("бот остановлен")
}
