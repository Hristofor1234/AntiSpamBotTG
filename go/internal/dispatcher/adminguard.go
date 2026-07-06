package dispatcher

import (
	"context"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// adminCacheTTL — как долго доверяем списку админов чата, прежде чем
// перезапросить его у Telegram заново. Админы меняются редко, а
// GetChatAdministrators дороже, чем проверка одной map — поэтому кэшируем,
// а не спрашиваем Telegram на каждое сообщение.
const adminCacheTTL = 5 * time.Minute

type adminCacheEntry struct {
	ids       map[int64]struct{}
	expiresAt time.Time
}

// adminGuard хранит закэшированные списки админов/владельца по чатам —
// нужно, чтобы автоматические уровни обороны (флуд, глобальный ЧС, опасные
// домены, триггер-слова) никогда не пытались забанить админа или владельца
// чата. Telegram и так не даст забанить владельца (вернёт ошибку "can't
// remove chat owner"), но без этой проверки бот на каждый флуд от владельца
// будет молча удалять его сообщение и получать одну и ту же бесполезную
// ошибку — а обычного админа, которого забанить технически можно,
// действительно забанит, что тоже нежелательно для автоматической модерации.
type adminGuard struct {
	mu      sync.Mutex
	entries map[int64]adminCacheEntry
}

func newAdminGuard() *adminGuard {
	return &adminGuard{entries: make(map[int64]adminCacheEntry)}
}

// isProtected — является ли userID админом или владельцем чата chatID.
// При ошибке запроса к Telegram считаем, что не защищён (fail-open) —
// как и остальные проверки в processUpdate, ошибка внешнего сервиса не
// должна блокировать модерацию, а её причина сама попадёт в лог.
func (g *adminGuard) isProtected(ctx context.Context, b *tgbot.Bot, chatID, userID int64) bool {
	g.mu.Lock()
	entry, ok := g.entries[chatID]
	g.mu.Unlock()

	if !ok || time.Now().After(entry.expiresAt) {
		members, err := b.GetChatAdministrators(ctx, &tgbot.GetChatAdministratorsParams{ChatID: chatID})
		if err != nil {
			return false
		}

		ids := make(map[int64]struct{}, len(members))
		for _, m := range members {
			if id, ok := chatMemberUserID(m); ok {
				ids[id] = struct{}{}
			}
		}

		entry = adminCacheEntry{ids: ids, expiresAt: time.Now().Add(adminCacheTTL)}
		g.mu.Lock()
		g.entries[chatID] = entry
		g.mu.Unlock()
	}

	_, protected := entry.ids[userID]
	return protected
}

// chatMemberUserID достаёт ID пользователя из ChatMember — GetChatAdministrators
// возвращает только владельца и администраторов, поэтому проверяем только эти
// два случая (остальные ветки ChatMember сюда прийти не должны).
func chatMemberUserID(m models.ChatMember) (int64, bool) {
	switch m.Type {
	case models.ChatMemberTypeOwner:
		if m.Owner != nil && m.Owner.User != nil {
			return m.Owner.User.ID, true
		}
	case models.ChatMemberTypeAdministrator:
		if m.Administrator != nil {
			return m.Administrator.User.ID, true
		}
	}
	return 0, false
}
