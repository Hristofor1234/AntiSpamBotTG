// Package captcha реализует проверку "не бот" для новых участников чата:
// сразу после вступления пользователь ограничивается в отправке сообщений и
// получает приглашение нажать кнопку "Я не бот". Не нажал вовремя —
// удаляется из чата (кик, не постоянный бан). Нажал — ограничение снимается.
//
// Триггером служит явное событие вступления в чат (NewChatMembers в
// апдейте от Telegram), а не эвристика вроде "первое сообщение от
// пользователя" — иначе после каждого перезапуска бота все давние участники
// чата выглядели бы "новыми" и массово получали бы капчу на следующее же
// сообщение. Telegram Bot API вообще не отдаёт дату регистрации аккаунта,
// поэтому "аккаунт младше N дней" технически не проверить — а вот "только
// что вступил в этот чат" — надёжный и единственно доступный сигнал.
package captcha

import (
	"sync"
	"time"
)

type key struct {
	ChatID int64
	UserID int64
}

type pendingEntry struct {
	challengeMessageID int
	timer              *time.Timer
}

// Manager отслеживает участников, ожидающих прохождения капчи. Безопасен
// для конкурентного использования.
type Manager struct {
	mu      sync.Mutex
	pending map[key]*pendingEntry
	timeout time.Duration
}

// New создаёт менеджер капчи: не подтвердил за timeout — считается ботом.
func New(timeout time.Duration) *Manager {
	return &Manager{
		pending: make(map[key]*pendingEntry),
		timeout: timeout,
	}
}

// IsPending — ожидает ли этот пользователь в этом чате прохождения капчи.
// Используется, чтобы молча отбрасывать его сообщения до подтверждения
// (дополнительная подстраховка сверх ограничения прав в самом Telegram).
func (m *Manager) IsPending(chatID, userID int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.pending[key{ChatID: chatID, UserID: userID}]
	return ok
}

// Track регистрирует нового участника, ожидающего прохождения капчи.
// challengeMessageID — ID сообщения с кнопкой (пригодится вызывающему коду,
// чтобы удалить его после результата). Если пользователь не вызовет Resolve
// в течение Timeout, будет ровно один раз вызван onTimeout — если к этому
// моменту запись ещё не была снята через Resolve.
func (m *Manager) Track(chatID, userID int64, challengeMessageID int, onTimeout func()) {
	k := key{ChatID: chatID, UserID: userID}

	timer := time.AfterFunc(m.timeout, func() {
		m.mu.Lock()
		_, stillPending := m.pending[k]
		delete(m.pending, k)
		m.mu.Unlock()

		if stillPending {
			onTimeout()
		}
	})

	m.mu.Lock()
	m.pending[k] = &pendingEntry{challengeMessageID: challengeMessageID, timer: timer}
	m.mu.Unlock()
}

// Resolve отмечает капчу пройденной: останавливает таймер и снимает
// ожидание. Возвращает ID сообщения с кнопкой и true — если запись
// действительно была активна (защита от повторных или чужих нажатий на уже
// обработанную кнопку).
func (m *Manager) Resolve(chatID, userID int64) (challengeMessageID int, ok bool) {
	k := key{ChatID: chatID, UserID: userID}

	m.mu.Lock()
	defer m.mu.Unlock()

	p, exists := m.pending[k]
	if !exists {
		return 0, false
	}

	p.timer.Stop()
	delete(m.pending, k)
	return p.challengeMessageID, true
}
