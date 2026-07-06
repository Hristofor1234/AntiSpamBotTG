package dispatcher

import (
	"context"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// ownerCacheTTL — как долго доверяем ID владельца чата, прежде чем
// перезапросить его у Telegram заново. Владелец меняется только при передаче
// прав в самом Telegram (крайне редко), а GetChatAdministrators дороже, чем
// сравнение одного int64 — поэтому кэшируем, а не спрашиваем на каждое
// сообщение.
const ownerCacheTTL = 5 * time.Minute

type ownerCacheEntry struct {
	ownerID   int64
	found     bool
	expiresAt time.Time
}

// ownerGuard хранит закэшированный ID владельца по чатам — нужно только для
// того, чтобы бан (см. Dispatcher.ban) не пытался забанить владельца чата:
// Telegram и так вернёт ошибку "can't remove chat owner" на banChatMember,
// так что бессмысленный API-вызов и повторяющуюся ошибку в логах на каждое
// срабатывание проще пропустить заранее, зная ID владельца.
//
// Обычные администраторы чата под эту проверку не подпадают: все уровни
// автомодерации (флуд, глобальный ЧС, опасные домены, триггер-слова)
// применяются к ним наравне с остальными участниками, и забанить их бот
// технически может — это осознанное поведение, не баг.
type ownerGuard struct {
	mu      sync.Mutex
	entries map[int64]ownerCacheEntry
}

func newOwnerGuard() *ownerGuard {
	return &ownerGuard{entries: make(map[int64]ownerCacheEntry)}
}

// isOwner — является ли userID владельцем чата chatID. При ошибке запроса к
// Telegram считаем, что не владелец (fail-open) — как и остальные проверки в
// processUpdate, ошибка внешнего сервиса не должна блокировать модерацию, а
// её причина сама попадёт в лог через withRetry в самом ban().
func (g *ownerGuard) isOwner(ctx context.Context, b *tgbot.Bot, chatID, userID int64) bool {
	g.mu.Lock()
	entry, ok := g.entries[chatID]
	g.mu.Unlock()

	if !ok || time.Now().After(entry.expiresAt) {
		members, err := b.GetChatAdministrators(ctx, &tgbot.GetChatAdministratorsParams{ChatID: chatID})
		if err != nil {
			return false
		}

		entry = ownerCacheEntry{expiresAt: time.Now().Add(ownerCacheTTL)}
		for _, m := range members {
			if m.Type == models.ChatMemberTypeOwner && m.Owner != nil && m.Owner.User != nil {
				entry.ownerID = m.Owner.User.ID
				entry.found = true
				break
			}
		}

		g.mu.Lock()
		g.entries[chatID] = entry
		g.mu.Unlock()
	}

	return entry.found && entry.ownerID == userID
}
