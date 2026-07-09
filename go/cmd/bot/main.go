// Команда bot — точка входа антифлуд-бота для Telegram.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"

	"github.com/Hristofor1234/AntiSpamBotTG/internal/captcha"
	"github.com/Hristofor1234/AntiSpamBotTG/internal/config"
	"github.com/Hristofor1234/AntiSpamBotTG/internal/dispatcher"
	"github.com/Hristofor1234/AntiSpamBotTG/internal/ratelimit"
	"github.com/Hristofor1234/AntiSpamBotTG/internal/reports"
	"github.com/Hristofor1234/AntiSpamBotTG/internal/storage"
	"github.com/Hristofor1234/AntiSpamBotTG/internal/tglog"
)

// dbInitTimeout — сколько ждём подключения к PostgreSQL при старте
// (storage.New сам делает несколько попыток Ping с паузами — таймаут должен
// с запасом их покрывать).
const dbInitTimeout = 30 * time.Second

func main() {
	stdoutHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(stdoutHandler)

	// .env нужен только для локального запуска без Docker; в контейнере
	// переменные приходят из окружения, и файла может не быть — это не ошибка.
	_ = godotenv.Load()

	if bootstrapLogger, closeBootstrapLogger := bootstrapTelegramErrorLogger(stdoutHandler); bootstrapLogger != nil {
		logger = bootstrapLogger
		defer closeBootstrapLogger()
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		logger.Error("ошибка конфигурации", "error", err)
		os.Exit(1)
	}

	if cfg.ErrorLogChatID != 0 {
		logger.Info("отправка error-логов в Telegram включена",
			"chat_id", cfg.ErrorLogChatID,
			"message_thread_id", cfg.ErrorLogMessageThreadID)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	limiter := ratelimit.New(cfg.RateLimitCount, cfg.RateLimitWindow)
	go limiter.RunCleanup(ctx, time.Minute)

	// Обучение через жалобы: /report в ответ на сообщение засчитывается как
	// голос; когда наберётся cfg.ReportThreshold разных жалобщиков — бан +
	// (если БД подключена) обучение глобального чёрного списка.
	reportTracker := reports.New(cfg.ReportThreshold)
	go reportTracker.RunCleanup(ctx, time.Hour)

	// Капча для новых участников чата: сразу после вступления пользователь
	// мутится и должен подтвердить, что не бот, за cfg.CaptchaTimeout.
	// Менеджер создаём всегда (дёшево), а использование включаем/выключаем
	// через cfg.CaptchaEnabled в местах вызова.
	captchaManager := captcha.New(cfg.CaptchaTimeout)

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
		msg := moderationMessageFromUpdate(update)
		if msg == nil {
			return
		}

		// Новые участники чата — капча, а не антифлуд-конвейер.
		if update.Message != nil && cfg.CaptchaEnabled && len(msg.NewChatMembers) > 0 {
			handleNewChatMembers(ctx, b, captchaManager, cfg, logger, msg.Chat.ID, msg.NewChatMembers)
			return
		}

		if msg.From == nil {
			return
		}
		from := msg.From
		if from.IsBot {
			return
		}

		if cfg.CaptchaEnabled && captchaManager.IsPending(msg.Chat.ID, from.ID) {
			// Ограничение прав в Telegram уже должно не пускать такие
			// сообщения, но подчищаем на случай гонки/базовых групп, где
			// ограничения соблюдаются не так строго, как в супергруппах.
			_, _ = b.DeleteMessage(ctx, &tgbot.DeleteMessageParams{
				ChatID:    msg.Chat.ID,
				MessageID: msg.ID,
			})
			return
		}

		d.Submit(dispatcher.Update{
			MessageID: msg.ID,
			ChatID:    msg.Chat.ID,
			UserID:    from.ID,
			Username:  from.Username,
			Text:      messageModerationText(msg),
			Timestamp: time.Unix(int64(msg.Date), 0),
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

	// Подсказки команд при вводе "/" в Telegram — отдельный список для групп
	// (все команды) и личных чатов (только /start, /help).
	registerBotCommands(ctx, b, logger)

	mode := "Long Polling"
	if cfg.WebhookURL != "" {
		mode = "Webhook"
	}

	// /start и /help показывают один и тот же текст (sendHelp, см. help.go):
	// полный список функций бота и текущие настройки. dbConnected — реальное
	// состояние подключения (store != nil), а не просто "задан ли
	// DATABASE_URL": БД могла быть недоступна при старте (см. выше).
	dbConnected := store != nil
	helpHandler := func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
		if update.Message == nil {
			return
		}
		sendHelp(ctx, b, update.Message, cfg, store, mode, dbConnected, logger)
	}

	registerCommand(b, "start", helpHandler)
	registerCommand(b, "help", helpHandler)

	// /report — обучение через жалобы сообщества: отправляется в ответ
	// (reply) на подозрительное сообщение. Как только на одно и то же
	// сообщение пожалуются cfg.ReportThreshold разных пользователей, автор
	// банится, а текст уходит в глобальное обучение — так же, как при бане
	// за флуд (см. dispatcher.BanSpammer).
	registerCommand(b, "report",
		func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
			msg := update.Message
			if msg == nil || msg.From == nil {
				return
			}

			target := msg.ReplyToMessage
			if target == nil || target.From == nil {
				sendAndScheduleDelete(ctx, b, msg,
					"Команду /report нужно отправить в ответ (reply) на сообщение, которое считаете спамом.",
					"", cfg.AutoDeleteDelay, logger)
				return
			}
			if target.From.IsBot {
				return
			}
			if target.From.ID == msg.From.ID {
				sendAndScheduleDelete(ctx, b, msg, "Нельзя пожаловаться на собственное сообщение.",
					"", cfg.AutoDeleteDelay, logger)
				return
			}

			count, triggered := reportTracker.Report(msg.Chat.ID, target.ID, msg.From.ID)
			if triggered {
				logger.Warn("сообщение забанено по жалобам сообщества",
					"chat_id", msg.Chat.ID, "message_id", target.ID,
					"author_id", target.From.ID, "reports", count)
				d.BanSpammer(ctx, msg.Chat.ID, target.ID, target.From.ID, target.From.Username, messageModerationText(target))
				return
			}

			sendAndScheduleDelete(ctx, b, msg,
				fmt.Sprintf("Жалоба принята (%d/%d).", count, cfg.ReportThreshold),
				"", cfg.AutoDeleteDelay, logger)
		},
	)

	if cfg.CaptchaEnabled {
		registerCaptchaCallbackHandler(b, captchaManager, logger)
	}

	// d нужен обработчику /ban (registerAdminHandlers ниже) — присваиваем до
	// регистрации хендлеров, а не после, как раньше: захват указателя d по
	// замыканию (как в defaultHandler) сработал бы и с nil на момент
	// регистрации, но передача d обычным параметром функции — нет, это уже
	// копия значения на момент вызова.
	d = dispatcher.New(
		cfg.WorkerCount,
		cfg.QueueSize,
		limiter,
		b,
		store,
		cfg.SilentBan,
		cfg.WarnThreshold,
		cfg.ContentFilterEnabled,
		cfg.ContentFilterAction,
		cfg.ContentFilterAllowSubstrings,
		logger,
	)
	d.Start(ctx)

	// /addspam, /removespam, /triggers, /blockdomain, /unblockdomain,
	// /domains, /ban — административные команды, для текущего чата.
	registerAdminHandlers(b, store, d, cfg.AutoDeleteDelay, logger)

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

func bootstrapTelegramErrorLogger(stdoutHandler slog.Handler) (*slog.Logger, func()) {
	botToken := strings.TrimSpace(os.Getenv("BOT_TOKEN"))
	if botToken == "" {
		return nil, nil
	}

	chatIDRaw := strings.TrimSpace(os.Getenv("ERROR_LOG_CHAT_ID"))
	if chatIDRaw == "" {
		return nil, nil
	}

	chatID, err := strconv.ParseInt(chatIDRaw, 10, 64)
	if err != nil {
		return nil, nil
	}

	threadID := 0
	threadIDRaw := strings.TrimSpace(os.Getenv("ERROR_LOG_MESSAGE_THREAD_ID"))
	if threadIDRaw != "" {
		parsedThreadID, err := strconv.Atoi(threadIDRaw)
		if err != nil {
			return nil, nil
		}
		threadID = parsedThreadID
	}

	remindInterval := 30 * time.Minute
	if reminderRaw := strings.TrimSpace(os.Getenv("ERROR_LOG_REMINDER_MINUTES")); reminderRaw != "" {
		if minutes, err := strconv.Atoi(reminderRaw); err == nil && minutes > 0 {
			remindInterval = time.Duration(minutes) * time.Minute
		}
	}

	errorLogHandler, closeErrorLogHandler, err := tglog.NewAsyncHandler(
		botToken,
		chatID,
		threadID,
		"AntiSpamBotTG",
		slog.LevelInfo,
		remindInterval,
	)
	if err != nil {
		return nil, nil
	}

	return slog.New(tglog.NewMultiHandler(stdoutHandler, errorLogHandler)), closeErrorLogHandler
}
