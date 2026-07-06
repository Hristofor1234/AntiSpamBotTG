package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Hristofor1234/AntiSpamBotTG/internal/corewords"
	"github.com/Hristofor1234/AntiSpamBotTG/internal/dispatcher"
	"github.com/Hristofor1234/AntiSpamBotTG/internal/storage"
)

// isChatAdmin проверяет, является ли userID администратором или создателем
// чата chatID — /addspam и /removespam должны быть доступны только
// модераторам, а не любому участнику чата.
func isChatAdmin(ctx context.Context, b *tgbot.Bot, chatID, userID int64) (bool, error) {
	member, err := b.GetChatMember(ctx, &tgbot.GetChatMemberParams{ChatID: chatID, UserID: userID})
	if err != nil {
		return false, err
	}
	return member.Type == models.ChatMemberTypeOwner || member.Type == models.ChatMemberTypeAdministrator, nil
}

// commandArgument возвращает текст команды без самой команды: например,
// для "/addspam казино" при паттерне "addspam" вернёт "казино". Ищет
// entity бота-команды в сообщении, поэтому корректно работает и с
// "/addspam@BotUsername казино".
func commandArgument(msg *models.Message) string {
	for _, e := range msg.Entities {
		if e.Type == models.MessageEntityTypeBotCommand && e.Offset+e.Length <= len(msg.Text) {
			return strings.TrimSpace(msg.Text[e.Offset+e.Length:])
		}
	}
	return ""
}

// registerAdminHandlers регистрирует административные команды: /addspam,
// /removespam, /triggers, /blockdomain, /unblockdomain, /domains (требуют
// подключённой PostgreSQL — без неё отвечают понятным сообщением, а не
// молчат) и /ban (работает и без БД — обучение при бане просто не
// произойдёт, см. internal/dispatcher.ban). autoDeleteDelay — через сколько
// удалять и саму команду, и ответ бота в групповых чатах (0 — не удалять),
// см. autodelete.go.
func registerAdminHandlers(b *tgbot.Bot, store *storage.Store, d *dispatcher.Dispatcher, autoDeleteDelay time.Duration, logger *slog.Logger) {
	reply := func(ctx context.Context, msg *models.Message, text string) {
		sendAndScheduleDelete(ctx, b, msg, text, "", autoDeleteDelay, logger)
	}

	// requireAdmin возвращает true, если можно продолжать обработку команды
	// (отправитель — админ чата); иначе сама отвечает пользователю и
	// возвращает false.
	requireAdmin := func(ctx context.Context, msg *models.Message) bool {
		if msg.From == nil {
			return false
		}
		admin, err := isChatAdmin(ctx, b, msg.Chat.ID, msg.From.ID)
		if err != nil {
			logger.Error("не удалось проверить права администратора",
				"error", err, "chat_id", msg.Chat.ID, "user_id", msg.From.ID)
			reply(ctx, msg, "Не удалось проверить права администратора, попробуйте позже.")
			return false
		}
		if !admin {
			reply(ctx, msg, "Эта команда доступна только администраторам чата.")
			return false
		}
		return true
	}

	registerCommand(b, "addspam",
		func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
			msg := update.Message
			if msg == nil {
				return
			}
			if store == nil {
				reply(ctx, msg, "Фильтр по словам недоступен: не подключена PostgreSQL (DATABASE_URL).")
				return
			}
			if !requireAdmin(ctx, msg) {
				return
			}

			phrase := commandArgument(msg)
			if phrase == "" {
				reply(ctx, msg, "Использование: /addspam <слово или фраза>")
				return
			}

			if err := store.AddTrigger(ctx, msg.Chat.ID, phrase); err != nil {
				logger.Error("не удалось добавить триггер", "error", err, "chat_id", msg.Chat.ID)
				reply(ctx, msg, "Не удалось сохранить фразу, попробуйте позже.")
				return
			}
			reply(ctx, msg, fmt.Sprintf("Добавлено в фильтр: «%s»", phrase))
		},
	)

	registerCommand(b, "removespam",
		func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
			msg := update.Message
			if msg == nil {
				return
			}
			if store == nil {
				reply(ctx, msg, "Фильтр по словам недоступен: не подключена PostgreSQL (DATABASE_URL).")
				return
			}
			if !requireAdmin(ctx, msg) {
				return
			}

			phrase := commandArgument(msg)
			if phrase == "" {
				reply(ctx, msg, "Использование: /removespam <слово или фраза>")
				return
			}

			removed, err := store.RemoveTrigger(ctx, msg.Chat.ID, phrase)
			if err != nil {
				logger.Error("не удалось удалить триггер", "error", err, "chat_id", msg.Chat.ID)
				reply(ctx, msg, "Не удалось удалить фразу, попробуйте позже.")
				return
			}
			if !removed {
				reply(ctx, msg, fmt.Sprintf("«%s» и не было в фильтре.", phrase))
				return
			}
			reply(ctx, msg, fmt.Sprintf("Удалено из фильтра: «%s»", phrase))
		},
	)

	registerCommand(b, "triggers",
		func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
			msg := update.Message
			if msg == nil {
				return
			}
			if store == nil {
				reply(ctx, msg, "Фильтр по словам недоступен: не подключена PostgreSQL (DATABASE_URL).")
				return
			}
			if !requireAdmin(ctx, msg) {
				return
			}

			phrases, err := store.ListTriggers(ctx, msg.Chat.ID)
			if err != nil {
				logger.Error("не удалось получить список триггеров", "error", err, "chat_id", msg.Chat.ID)
				reply(ctx, msg, "Не удалось получить список, попробуйте позже.")
				return
			}
			if len(phrases) == 0 {
				reply(ctx, msg, "Фильтр по словам для этого чата пуст.")
				return
			}
			reply(ctx, msg, "Триггер-фразы этого чата:\n— "+strings.Join(phrases, "\n— "))
		},
	)

	// /addcorewords — быстро наполнить фильтр слов текущего чата встроенной
	// категорией (internal/corewords), не вводя каждое слово вручную через
	// /addspam. Ядро неизменно и живёт в коде; сами слова после добавления
	// хранятся как обычные записи chat_triggers этого чата — их можно
	// убрать через /removespam, как и любую другую фразу.
	registerCommand(b, "addcorewords",
		func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
			msg := update.Message
			if msg == nil {
				return
			}
			if store == nil {
				reply(ctx, msg, "Фильтр по словам недоступен: не подключена PostgreSQL (DATABASE_URL).")
				return
			}
			if !requireAdmin(ctx, msg) {
				return
			}

			category := strings.ToLower(strings.TrimSpace(commandArgument(msg)))
			if category == "" {
				reply(ctx, msg, fmt.Sprintf(
					"Использование: /addcorewords <категория>\nДоступные категории: %s, all",
					strings.Join(corewords.Order, ", "),
				))
				return
			}

			var words []string
			if category == "all" {
				for _, name := range corewords.Order {
					words = append(words, corewords.Categories[name]...)
				}
			} else if list, ok := corewords.Categories[category]; ok {
				words = list
			} else {
				reply(ctx, msg, fmt.Sprintf(
					"Неизвестная категория «%s». Доступные: %s, all",
					category, strings.Join(corewords.Order, ", "),
				))
				return
			}

			for _, word := range words {
				if err := store.AddTrigger(ctx, msg.Chat.ID, word); err != nil {
					logger.Error("не удалось добавить слово из ядра", "error", err, "chat_id", msg.Chat.ID, "word", word)
					reply(ctx, msg, "Не удалось добавить часть слов, попробуйте позже.")
					return
				}
			}
			reply(ctx, msg, fmt.Sprintf("Добавлено %d слов из категории «%s» (уже существовавшие пропущены).", len(words), category))
		},
	)

	// /blockdomain, /unblockdomain, /domains — ручное управление глобальным
	// списком опасных доменов (internal/linkcheck), в дополнение к
	// автоматическому обучению на банах. Список глобальный (как
	// global_blacklist), поэтому доступен из любого чата — тот же принцип,
	// что и у автообучения: бан в одном чате уже сейчас влияет на все
	// остальные чаты бота.
	registerCommand(b, "blockdomain",
		func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
			msg := update.Message
			if msg == nil {
				return
			}
			if store == nil {
				reply(ctx, msg, "Проверка ссылок недоступна: не подключена PostgreSQL (DATABASE_URL).")
				return
			}
			if !requireAdmin(ctx, msg) {
				return
			}

			domain := strings.ToLower(strings.TrimSpace(commandArgument(msg)))
			domain = strings.TrimPrefix(domain, "www.")
			if domain == "" {
				reply(ctx, msg, "Использование: /blockdomain <домен> (например: spam-casino.xyz)")
				return
			}

			if err := store.LearnDomains(ctx, []string{domain}, "добавлено вручную через /blockdomain"); err != nil {
				logger.Error("не удалось добавить домен", "error", err, "chat_id", msg.Chat.ID)
				reply(ctx, msg, "Не удалось сохранить домен, попробуйте позже.")
				return
			}
			reply(ctx, msg, fmt.Sprintf("Домен «%s» добавлен в глобальный список опасных.", domain))
		},
	)

	registerCommand(b, "unblockdomain",
		func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
			msg := update.Message
			if msg == nil {
				return
			}
			if store == nil {
				reply(ctx, msg, "Проверка ссылок недоступна: не подключена PostgreSQL (DATABASE_URL).")
				return
			}
			if !requireAdmin(ctx, msg) {
				return
			}

			domain := commandArgument(msg)
			if domain == "" {
				reply(ctx, msg, "Использование: /unblockdomain <домен>")
				return
			}

			removed, err := store.RemoveDomain(ctx, domain)
			if err != nil {
				logger.Error("не удалось удалить домен", "error", err, "chat_id", msg.Chat.ID)
				reply(ctx, msg, "Не удалось удалить домен, попробуйте позже.")
				return
			}
			if !removed {
				reply(ctx, msg, fmt.Sprintf("«%s» и не было в списке опасных.", domain))
				return
			}
			reply(ctx, msg, fmt.Sprintf("Домен «%s» удалён из глобального списка опасных.", domain))
		},
	)

	registerCommand(b, "domains",
		func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
			msg := update.Message
			if msg == nil {
				return
			}
			if store == nil {
				reply(ctx, msg, "Проверка ссылок недоступна: не подключена PostgreSQL (DATABASE_URL).")
				return
			}
			if !requireAdmin(ctx, msg) {
				return
			}

			const recentLimit = 30
			domains, err := store.ListRecentDomains(ctx, recentLimit)
			if err != nil {
				logger.Error("не удалось получить список опасных доменов", "error", err, "chat_id", msg.Chat.ID)
				reply(ctx, msg, "Не удалось получить список, попробуйте позже.")
				return
			}
			if len(domains) == 0 {
				reply(ctx, msg, "Глобальный список опасных доменов пуст.")
				return
			}
			reply(ctx, msg, fmt.Sprintf("Последние опасные домены (до %d, список общий для всех чатов):\n— %s",
				recentLimit, strings.Join(domains, "\n— ")))
		},
	)

	// /ban — ручной бан администратором чата, в ответ (reply) на сообщение
	// спамера. В отличие от автоматических уровней обороны (rate-limit,
	// триггер-слова, глобальный ЧС), это единственный способ научить бота
	// на спам, который он сам не отловил (например, ссылка на ещё
	// неизвестный домен или разовое сообщение, не подпадающее под флуд).
	// BanSpammer банит и одновременно обучает те же global_blacklist и
	// bad_domains, что и автоматический бан за флуд/жалобы — отдельно
	// ничего обучать не нужно, d.BanSpammer уже делает всё через ban().
	registerCommand(b, "ban",
		func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
			msg := update.Message
			if msg == nil {
				return
			}
			if !requireAdmin(ctx, msg) {
				return
			}

			target := msg.ReplyToMessage
			if target == nil || target.From == nil {
				reply(ctx, msg, "Команду /ban нужно отправить в ответ (reply) на сообщение спамера.")
				return
			}
			if target.From.IsBot {
				reply(ctx, msg, "Нельзя забанить бота.")
				return
			}
			if target.From.ID == msg.From.ID {
				reply(ctx, msg, "Нельзя забанить самого себя.")
				return
			}

			logger.Info("ручной бан администратором",
				"admin_id", msg.From.ID, "spammer_id", target.From.ID, "chat_id", msg.Chat.ID)

			// Уведомление о бане (если не SILENT_BAN) отправляет само
			// d.BanSpammer/ban — второе сообщение здесь не нужно и
			// дублировало бы его.
			d.BanSpammer(ctx, msg.Chat.ID, target.ID, target.From.ID, target.From.Username, target.Text)
		},
	)
}
