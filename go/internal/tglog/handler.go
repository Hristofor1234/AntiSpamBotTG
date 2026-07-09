package tglog

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
)

const (
	maxMessageLen       = 4096
	sendTimeout         = 5 * time.Second
	reminderCheckPeriod = time.Minute
)

type AsyncHandler struct {
	minLevel       slog.Leveler
	source         string
	notifier       *notifier
	remindInterval time.Duration

	mu        sync.Mutex
	incidents map[string]*incidentState

	attrs  []slog.Attr
	groups []string
}

type notifier struct {
	bot      *tgbot.Bot
	chatID   int64
	threadID int
	queue    chan string
	stop     chan struct{}
	wg       sync.WaitGroup
}

type incidentState struct {
	key      string
	title    string
	details  []string
	lastSent time.Time
	active   bool
}

type incidentEvent struct {
	key     string
	title   string
	details []string
}

func NewAsyncHandler(botToken string, chatID int64, threadID int, source string, minLevel slog.Leveler, remindInterval time.Duration) (*AsyncHandler, func(), error) {
	b, err := tgbot.New(botToken)
	if err != nil {
		return nil, nil, fmt.Errorf("create telegram log bot: %w", err)
	}

	n := &notifier{
		bot:      b,
		chatID:   chatID,
		threadID: threadID,
		queue:    make(chan string, 100),
		stop:     make(chan struct{}),
	}
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		for {
			select {
			case <-n.stop:
				return
			case msg, ok := <-n.queue:
				if !ok {
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
				_, err := n.bot.SendMessage(ctx, &tgbot.SendMessageParams{
					ChatID:          n.chatID,
					MessageThreadID: n.threadID,
					Text:            msg,
				})
				cancel()
				if err != nil {
					fmt.Fprintf(os.Stderr, "tglog send failed: chat_id=%d thread_id=%d error=%v\n", n.chatID, n.threadID, err)
				}
			}
		}
	}()

	h := &AsyncHandler{
		minLevel:       minLevel,
		source:         source,
		notifier:       n,
		remindInterval: remindInterval,
		incidents:      make(map[string]*incidentState),
	}

	if remindInterval > 0 {
		n.wg.Add(1)
		go func() {
			defer n.wg.Done()
			ticker := time.NewTicker(reminderCheckPeriod)
			defer ticker.Stop()
			for {
				select {
				case <-n.stop:
					return
				case <-ticker.C:
					h.sendDueReminders()
				}
			}
		}()
	}

	closeFn := func() {
		close(n.stop)
		close(n.queue)
		n.wg.Wait()
	}
	return h, closeFn, nil
}

func (h *AsyncHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel.Level()
}

func (h *AsyncHandler) Handle(_ context.Context, r slog.Record) error {
	fields := collectFields(h.attrs, h.groups)
	r.Attrs(func(attr slog.Attr) bool {
		for k, v := range collectFields([]slog.Attr{attr}, h.groups) {
			fields[k] = v
		}
		return true
	})

	if r.Level >= slog.LevelError {
		if event := classifyErrorEvent(r.Message, fields); event != nil {
			h.upsertIncident(*event)
			return nil
		}
	}

	if event := classifyRecoveryEvent(r.Message); event != nil {
		h.resolveIncident(*event)
	}

	return nil
}

func (h *AsyncHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cloned := &AsyncHandler{
		minLevel:       h.minLevel,
		source:         h.source,
		notifier:       h.notifier,
		remindInterval: h.remindInterval,
		incidents:      h.incidents,
		attrs:          append(append([]slog.Attr(nil), h.attrs...), attrs...),
		groups:         append([]string(nil), h.groups...),
	}
	return cloned
}

func (h *AsyncHandler) WithGroup(name string) slog.Handler {
	cloned := &AsyncHandler{
		minLevel:       h.minLevel,
		source:         h.source,
		notifier:       h.notifier,
		remindInterval: h.remindInterval,
		incidents:      h.incidents,
		attrs:          append([]slog.Attr(nil), h.attrs...),
		groups:         append(append([]string(nil), h.groups...), name),
	}
	return cloned
}

func (h *AsyncHandler) sendDueReminders() {
	if h.remindInterval <= 0 {
		return
	}

	now := time.Now()
	var due []incidentState

	h.mu.Lock()
	for _, state := range h.incidents {
		if !state.active || now.Sub(state.lastSent) < h.remindInterval {
			continue
		}
		state.lastSent = now
		due = append(due, *state)
	}
	h.mu.Unlock()

	for _, state := range due {
		h.enqueue(formatReminder(h.source, state.title, state.details))
	}
}

func (h *AsyncHandler) upsertIncident(event incidentEvent) {
	now := time.Now()

	h.mu.Lock()
	state, exists := h.incidents[event.key]
	if exists && state.active {
		state.title = event.title
		state.details = append([]string(nil), event.details...)
		h.mu.Unlock()
		return
	}

	h.incidents[event.key] = &incidentState{
		key:      event.key,
		title:    event.title,
		details:  append([]string(nil), event.details...),
		lastSent: now,
		active:   true,
	}
	h.mu.Unlock()

	h.enqueue(formatAlert(h.source, event.title, event.details))
}

func (h *AsyncHandler) resolveIncident(event incidentEvent) {
	h.mu.Lock()
	state, exists := h.incidents[event.key]
	if !exists || !state.active {
		h.mu.Unlock()
		return
	}
	state.active = false
	h.mu.Unlock()

	h.enqueue(formatResolved(h.source, event.title, event.details))
}

func (h *AsyncHandler) enqueue(msg string) {
	if len(msg) > maxMessageLen {
		msg = msg[:maxMessageLen-14] + "\n...truncated"
	}
	select {
	case h.notifier.queue <- msg:
	default:
	}
}

func classifyErrorEvent(message string, fields map[string]string) *incidentEvent {
	errorText := fields["error"]
	combined := strings.ToLower(strings.TrimSpace(message + "\n" + errorText))

	switch {
	case strings.Contains(combined, "postgresql") || strings.Contains(combined, "database_url") || strings.Contains(combined, "failed sasl auth") || strings.Contains(combined, "sqlstate"):
		details := []string{"Бот не смог подключиться к PostgreSQL."}
		switch {
		case strings.Contains(combined, "password authentication failed"):
			details = append(details, "Причина: неверный логин или пароль к базе данных.")
		case strings.Contains(combined, "connection refused"), strings.Contains(combined, "no route to host"), strings.Contains(combined, "i/o timeout"):
			details = append(details, "Причина: база недоступна по сети или не запущена.")
		default:
			details = append(details, "Причина: соединение с базой данных не установилось.")
		}
		return &incidentEvent{
			key:     "database",
			title:   "Проблема с базой данных",
			details: details,
		}

	case strings.Contains(combined, "telegram") && strings.Contains(combined, "429"):
		return &incidentEvent{
			key:   "telegram_rate_limit",
			title: "Telegram ограничил запросы",
			details: []string{
				"Бот уперся в лимиты Telegram.",
				"Обычно это временно и проходит само.",
			},
		}

	case strings.Contains(combined, "telegram"), strings.Contains(combined, "client timeout exceeded"), strings.Contains(combined, "context deadline exceeded"):
		return &incidentEvent{
			key:   "telegram_network",
			title: "Проблема с Telegram или сетью",
			details: []string{
				"Бот не смог достучаться до Telegram API.",
				"Проверь интернет на сервере, если ошибка повторяется.",
			},
		}

	case strings.Contains(combined, "queue_size"), strings.Contains(combined, "bot_token"), strings.Contains(combined, "rate_limit"), strings.Contains(combined, "content_filter"), strings.Contains(combined, "ошибка конфигурации"):
		return &incidentEvent{
			key:   "config",
			title: "Ошибка в настройках бота",
			details: []string{
				"Бот не смог прочитать `.env` или одну из переменных окружения.",
				shortConfigReason(errorText, message),
			},
		}

	case strings.Contains(combined, "chat not found"), strings.Contains(combined, "forbidden"):
		return &incidentEvent{
			key:   "log_chat_access",
			title: "Нет доступа к чату для логов",
			details: []string{
				"Бот не может отправить сообщение в Telegram-чат для ошибок.",
				"Проверь, что этот бот добавлен в нужную группу и может писать сообщения.",
			},
		}

	default:
		if message == "" {
			return nil
		}
		return &incidentEvent{
			key:   "generic:" + message,
			title: "Ошибка в работе бота",
			details: []string{
				shorten(message),
			},
		}
	}
}

func classifyRecoveryEvent(message string) *incidentEvent {
	normalized := strings.ToLower(strings.TrimSpace(message))
	switch {
	case strings.Contains(normalized, "подключение к postgresql установлено"):
		return &incidentEvent{
			key:   "database",
			title: "Проблема с базой данных решена",
			details: []string{
				"Подключение к PostgreSQL восстановлено.",
			},
		}
	case strings.Contains(normalized, "бот запущен"):
		return &incidentEvent{
			key:   "config",
			title: "Ошибка настроек решена",
			details: []string{
				"Бот снова запустился корректно.",
			},
		}
	default:
		return nil
	}
}

func formatAlert(source, title string, details []string) string {
	lines := []string{fmt.Sprintf("❌ %s | %s", source, title)}
	lines = append(lines, details...)
	return strings.Join(lines, "\n")
}

func formatReminder(source, title string, details []string) string {
	lines := []string{fmt.Sprintf("⚠️ %s | %s", source, title)}
	lines = append(lines, "Проблема всё ещё не решена.")
	lines = append(lines, details...)
	return strings.Join(lines, "\n")
}

func formatResolved(source, title string, details []string) string {
	lines := []string{fmt.Sprintf("✅ %s | %s", source, title)}
	lines = append(lines, details...)
	return strings.Join(lines, "\n")
}

func shortConfigReason(errorText, message string) string {
	combined := strings.TrimSpace(errorText + " " + message)
	switch {
	case strings.Contains(combined, "QUEUE_SIZE"):
		return "Причина: неправильно задана переменная `QUEUE_SIZE`."
	case strings.Contains(combined, "BOT_TOKEN"):
		return "Причина: не задан или неверно задан `BOT_TOKEN`."
	case strings.Contains(combined, "DATABASE_URL"):
		return "Причина: неправильно задан `DATABASE_URL`."
	default:
		return "Причина: одна из переменных окружения заполнена неверно."
	}
}

func collectFields(attrs []slog.Attr, groups []string) map[string]string {
	fields := make(map[string]string)
	for _, attr := range attrs {
		collectAttr(fields, groups, attr)
	}
	return fields
}

func collectAttr(fields map[string]string, groups []string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}

	key := attr.Key
	if len(groups) > 0 && key != "" {
		key = strings.Join(append(append([]string(nil), groups...), key), ".")
	}

	if attr.Value.Kind() == slog.KindGroup {
		nextGroups := append([]string(nil), groups...)
		if attr.Key != "" {
			nextGroups = append(nextGroups, attr.Key)
		}
		for _, nested := range attr.Value.Group() {
			collectAttr(fields, nextGroups, nested)
		}
		return
	}

	fields[key] = valueString(attr.Value)
}

func shorten(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Подробности смотри в логах контейнера."
	}
	if len(s) <= 220 {
		return s
	}
	return s[:217] + "..."
}

func valueString(v slog.Value) string {
	v = v.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindBool:
		if v.Bool() {
			return "true"
		}
		return "false"
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindFloat64:
		return fmt.Sprintf("%g", v.Float64())
	case slog.KindInt64:
		return fmt.Sprintf("%d", v.Int64())
	case slog.KindTime:
		return v.Time().UTC().Format(time.RFC3339)
	case slog.KindUint64:
		return fmt.Sprintf("%d", v.Uint64())
	default:
		return fmt.Sprint(v.Any())
	}
}
