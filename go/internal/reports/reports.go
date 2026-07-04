// Package reports реализует "обучение через жалобы" — третий источник
// обнаружения спама, дополняющий rate-limit и глобальный чёрный список
// (internal/storage). Если несколько разных участников чата пожаловались
// на одно и то же сообщение (командой /report в ответ на него), автор
// считается спамером: сообщение удаляется, автор банится, а текст уходит
// в глобальное обучение — так же, как при бане за флуд.
package reports

import (
	"context"
	"sync"
	"time"
)

// defaultTTL — как долго живёт запись о жалобах на сообщение, если порог
// так и не набрался. Без этого срока map рос бы бесконечно — та же беда,
// что и с in-memory счётчиками в ratelimit.Limiter, если её не чистить.
const defaultTTL = 24 * time.Hour

type key struct {
	ChatID    int64
	MessageID int
}

type record struct {
	reporters map[int64]struct{}
	createdAt time.Time
}

// Tracker считает уникальных "жалобщиков" на каждое сообщение и сообщает,
// когда их число достигает порога. Безопасен для конкурентного использования.
type Tracker struct {
	mu        sync.Mutex
	records   map[key]*record
	threshold int
	ttl       time.Duration
}

// New создаёт трекер жалоб: сообщение считается спамом, когда на него
// пожалуются threshold разных пользователей (threshold < 2 не имеет смысла —
// это должно проверяться на уровне конфигурации, см. internal/config).
func New(threshold int) *Tracker {
	return &Tracker{
		records:   make(map[key]*record),
		threshold: threshold,
		ttl:       defaultTTL,
	}
}

// Report регистрирует жалобу reporterID на сообщение messageID в чате
// chatID. Повторная жалоба от одного и того же пользователя не учитывается
// дважды (защита от накрутки одним аккаунтом). Возвращает текущее число
// уникальных жалобщиков и triggered=true ровно в момент, когда оно впервые
// достигло порога — после этого запись удаляется, чтобы не сработать
// повторно и не занимать память.
func (t *Tracker) Report(chatID int64, messageID int, reporterID int64) (count int, triggered bool) {
	k := key{ChatID: chatID, MessageID: messageID}

	t.mu.Lock()
	defer t.mu.Unlock()

	rec, ok := t.records[k]
	if !ok {
		rec = &record{reporters: make(map[int64]struct{}), createdAt: time.Now()}
		t.records[k] = rec
	}

	rec.reporters[reporterID] = struct{}{}
	count = len(rec.reporters)

	if count >= t.threshold {
		delete(t.records, k)
		return count, true
	}

	return count, false
}

// RunCleanup периодически удаляет записи о жалобах старше TTL — на случай,
// если порог так и не набрался. Блокируется до отмены ctx; запускать в
// отдельной горутине.
func (t *Tracker) RunCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.cleanup()
		}
	}
}

func (t *Tracker) cleanup() {
	cutoff := time.Now().Add(-t.ttl)

	t.mu.Lock()
	defer t.mu.Unlock()

	for k, rec := range t.records {
		if rec.createdAt.Before(cutoff) {
			delete(t.records, k)
		}
	}
}
