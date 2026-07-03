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
)

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

	wg sync.WaitGroup
}

// New создаёт диспетчер с workerCount воркерами и очередью на queueSize
// обновлений.
func New(workerCount, queueSize int, limiter *ratelimit.Limiter, b *tgbot.Bot, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		workerCount: workerCount,
		jobQueue:    make(chan Update, queueSize),
		limiter:     limiter,
		bot:         b,
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

// processUpdate — основная логика: проверка на флуд и, при превышении
// лимита, мгновенный бан с отзывом всех недавних сообщений пользователя.
func (d *Dispatcher) processUpdate(ctx context.Context, u Update) {
	if d.limiter.Allow(u.UserID) {
		d.logger.Info("сообщение принято", "user_id", u.UserID, "chat_id", u.ChatID)
		return
	}

	d.logger.Warn("обнаружен флуд, бан пользователя",
		"user_id", u.UserID, "username", u.Username, "chat_id", u.ChatID)

	if _, err := d.bot.DeleteMessage(ctx, &tgbot.DeleteMessageParams{
		ChatID:    u.ChatID,
		MessageID: u.MessageID,
	}); err != nil {
		d.logger.Error("не удалось удалить сообщение флудера",
			"error", err, "chat_id", u.ChatID, "message_id", u.MessageID)
	}

	// RevokeMessages: true — Telegram сам удаляет все недавние сообщения
	// забаненного пользователя в этом чате одним запросом.
	if _, err := d.bot.BanChatMember(ctx, &tgbot.BanChatMemberParams{
		ChatID:         u.ChatID,
		UserID:         u.UserID,
		RevokeMessages: true,
	}); err != nil {
		d.logger.Error("не удалось забанить флудера",
			"error", err, "chat_id", u.ChatID, "user_id", u.UserID)
		return
	}

	_, err := d.bot.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: u.ChatID,
		Text:   fmt.Sprintf("🚫 Пользователь %s забанен за флуд.", displayName(u)),
	})
	if err != nil {
		d.logger.Error("не удалось отправить уведомление о бане", "error", err)
	}
}

func displayName(u Update) string {
	if u.Username != "" {
		return "@" + u.Username
	}
	return fmt.Sprintf("id%d", u.UserID)
}
