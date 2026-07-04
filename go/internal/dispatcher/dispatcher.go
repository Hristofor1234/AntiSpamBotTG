// Package dispatcher разбирает входящие обновления Telegram в пуле
// воркеров, чтобы обработчик библиотеки go-telegram/bot никогда не
// блокировался на "тяжёлой" работе (проверка флуда, обращения к Telegram
// API на удаление сообщений и бан).
package dispatcher

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"

	"github.com/Hristofor1234/AntiSpamBotTG/internal/ratelimit"
	"github.com/Hristofor1234/AntiSpamBotTG/internal/storage"
)

// learnTimeout — таймаут на запись в глобальный чёрный список. Обучение
// выполняется в отдельной горутине на своём независимом контексте (не ctx
// воркера), чтобы медленная БД не задерживала обработку следующих сообщений
// и чтобы запись не обрывалась из-за отмены ctx при остановке бота.
const learnTimeout = 5 * time.Second

// Update — минимальный набор данных о сообщении, нужный диспетчеру.
// Не зависит от models.Update напрямую, чтобы диспетчер не был жёстко
// привязан к конкретной версии библиотеки бота.
type Update struct {
	MessageID int
	ChatID    int64
	UserID    int64
	Username  string
	Text      string
	Timestamp time.Time
}

// Dispatcher — пул воркеров, обрабатывающих очередь обновлений.
type Dispatcher struct {
	workerCount int
	jobQueue    chan Update
	limiter     *ratelimit.Limiter
	bot         *tgbot.Bot
	logger      *slog.Logger

	// store — глобальный чёрный список спама в PostgreSQL. Может быть nil:
	// если БД не настроена или была недоступна при старте, диспетчер просто
	// пропускает уровень глобальной проверки/обучения и работает только на
	// rate-limit — см. processUpdate.
	store *storage.Store

	wg sync.WaitGroup
}

// New создаёт диспетчер с workerCount воркерами и очередью на queueSize
// обновлений. store может быть nil — глобальное обучение тогда отключено.
func New(workerCount, queueSize int, limiter *ratelimit.Limiter, b *tgbot.Bot, store *storage.Store, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		workerCount: workerCount,
		jobQueue:    make(chan Update, queueSize),
		limiter:     limiter,
		bot:         b,
		store:       store,
		logger:      logger,
	}
}

// Start запускает воркеров в фоне. Возвращается сразу; воркеры работают,
// пока не отменится ctx.
func (d *Dispatcher) Start(ctx context.Context) {
	for i := 0; i < d.workerCount; i++ {
		d.wg.Add(1)
		go d.worker(ctx, i)
	}
}

// Wait блокируется, пока все воркеры не завершатся (используйте после
// отмены ctx для graceful shutdown).
func (d *Dispatcher) Wait() {
	d.wg.Wait()
}

// Submit кладёт обновление в очередь на обработку. Если очередь
// переполнена (аномальный всплеск нагрузки), обновление отбрасывается —
// это защищает бот от неограниченного роста памяти вместо падения
// с OutOfMemory.
func (d *Dispatcher) Submit(u Update) {
	select {
	case d.jobQueue <- u:
	default:
		d.logger.Warn("очередь обработки переполнена, обновление отброшено",
			"user_id", u.UserID, "chat_id", u.ChatID)
	}
}

func (d *Dispatcher) worker(ctx context.Context, id int) {
	defer d.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case u, ok := <-d.jobQueue:
			if !ok {
				return
			}
			d.processUpdate(ctx, u)
		}
	}
}

// processUpdate — три уровня обороны от спама:
//  1. глобальный чёрный список (PostgreSQL, если подключена) — мгновенный
//     отсев уже выученного спама, независимо от истории конкретного чата;
//  2. rate-limit (in-memory) — ловит новые, ещё не выученные вспышки флуда;
//  3. при бане по флуду текст сообщения уходит в обучение — в следующий раз
//     тот же (с точностью до нормализации) спам поймает уже уровень 1,
//     в любом чате, где стоит бот.
func (d *Dispatcher) processUpdate(ctx context.Context, u Update) {
	if d.store != nil && u.Text != "" {
		known, err := d.store.IsKnownSpam(ctx, u.Text)
		switch {
		case err != nil:
			// БД недоступна/споткнулась — деградируем до rate-limit-only,
			// не блокируем обработку сообщения из-за проблем с глобальным ЧС.
			d.logger.Error("не удалось проверить глобальный чёрный список, продолжаем без него",
				"error", err, "user_id", u.UserID, "chat_id", u.ChatID)
		case known:
			d.logger.Warn("сообщение совпало с глобальным чёрным списком спама",
				"user_id", u.UserID, "username", u.Username, "chat_id", u.ChatID)
			d.ban(ctx, u, false)
			return
		}
	}

	if d.limiter.Allow(u.UserID) {
		d.logger.Info("сообщение принято", "user_id", u.UserID, "chat_id", u.ChatID)
		return
	}

	d.logger.Warn("обнаружен флуд, бан пользователя",
		"user_id", u.UserID, "username", u.Username, "chat_id", u.ChatID)
	d.ban(ctx, u, true)
}

// ban удаляет сообщение и банит пользователя (с отзывом недавних сообщений в
// чате), затем уведомляет чат. Вызовы к Telegram API обёрнуты в withRetry:
// если Telegram отвечает 429 Too Many Requests (например, бот банит разом
// много флудеров), мы ждём RetryAfter и повторяем — иначе баны молча
// терялись бы.
//
// learn=true — текст добавляется в глобальный чёрный список (если БД
// подключена): в других чатах такой же спам будет отсечён мгновенно, ещё до
// rate-limit. Для бана по уже известному спаму (learn=false) переучивать
// нечего — запись уже есть.
func (d *Dispatcher) ban(ctx context.Context, u Update, learn bool) {
	if err := withRetry(ctx, d.logger, "deleteMessage", func() error {
		_, err := d.bot.DeleteMessage(ctx, &tgbot.DeleteMessageParams{
			ChatID:    u.ChatID,
			MessageID: u.MessageID,
		})
		return err
	}); err != nil {
		d.logger.Error("не удалось удалить сообщение флудера",
			"error", err, "chat_id", u.ChatID, "message_id", u.MessageID)
	}

	// RevokeMessages: true — Telegram сам удаляет все недавние сообщения
	// забаненного пользователя в этом чате одним запросом.
	if err := withRetry(ctx, d.logger, "banChatMember", func() error {
		_, err := d.bot.BanChatMember(ctx, &tgbot.BanChatMemberParams{
			ChatID:         u.ChatID,
			UserID:         u.UserID,
			RevokeMessages: true,
		})
		return err
	}); err != nil {
		d.logger.Error("не удалось забанить флудера",
			"error", err, "chat_id", u.ChatID, "user_id", u.UserID)
		return
	}

	if err := withRetry(ctx, d.logger, "sendMessage", func() error {
		_, err := d.bot.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: u.ChatID,
			Text:   fmt.Sprintf("🚫 Пользователь %s забанен за спам.", displayName(u)),
		})
		return err
	}); err != nil {
		d.logger.Error("не удалось отправить уведомление о бане", "error", err)
	}

	if !learn || d.store == nil || u.Text == "" {
		return
	}

	// Обучение — на отдельном контексте с собственным таймаутом: не должно
	// ни блокировать обработку следующих сообщений в очереди, ни обрываться
	// из-за отмены ctx воркера (например, при остановке бота).
	go func(text string) {
		learnCtx, cancel := context.WithTimeout(context.Background(), learnTimeout)
		defer cancel()

		if err := d.store.LearnSpam(learnCtx, text); err != nil {
			d.logger.Error("не удалось сохранить спам в глобальный чёрный список", "error", err)
		}
	}(u.Text)
}

func displayName(u Update) string {
	if u.Username != "" {
		return "@" + u.Username
	}
	return fmt.Sprintf("id%d", u.UserID)
}
