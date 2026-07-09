// Package ratelimit реализует потокобезопасный подсчёт частоты сообщений
// по каждому пользователю — основу для антифлуд-бана.
package ratelimit

import (
	"context"
	"sync"
	"time"
)

type key struct {
	ChatID int64
	UserID int64
}

// Limiter отслеживает временные метки последних сообщений каждого
// пользователя в каждом чате и решает, не превышен ли лимит "limit
// сообщений за window".
type Limiter struct {
	mu      sync.Mutex
	records map[key][]time.Time
	limit   int
	window  time.Duration
}

// New создаёт лимитер: не более limit сообщений за window.
func New(limit int, window time.Duration) *Limiter {
	return &Limiter{
		records: make(map[key][]time.Time),
		limit:   limit,
		window:  window,
	}
}

// Allow регистрирует очередное сообщение от userID в chatID и возвращает
// false, если пользователь превысил лимит сообщений за окно времени
// именно в этом чате (флуд).
// Безопасен для конкурентного вызова из нескольких воркеров.
func (l *Limiter) Allow(chatID, userID int64) bool {
	now := time.Now()
	cutoff := now.Add(-l.window)
	k := key{ChatID: chatID, UserID: userID}

	l.mu.Lock()
	defer l.mu.Unlock()

	fresh := filterAfter(l.records[k], cutoff)

	if len(fresh) >= l.limit {
		l.records[k] = fresh
		return false
	}

	l.records[k] = append(fresh, now)
	return true
}

// RunCleanup периодически удаляет из карты записи пользователей, которые
// не отправляли сообщений дольше окна лимита. Без этого map рос бы
// бесконечно при большом числе разных отправителей. Блокируется до
// отмены ctx — предполагается запуск в отдельной горутине.
func (l *Limiter) RunCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.cleanup()
		}
	}
}

func (l *Limiter) cleanup() {
	cutoff := time.Now().Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	for k, history := range l.records {
		if len(filterAfter(history, cutoff)) == 0 {
			delete(l.records, k)
		}
	}
}

// filterAfter возвращает элементы history, которые позже cutoff.
// Фильтрует на месте (переиспользует backing array history), поэтому
// после вызова исходный слайс history использовать не следует.
func filterAfter(history []time.Time, cutoff time.Time) []time.Time {
	fresh := history[:0]
	for _, t := range history {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}
	return fresh
}
