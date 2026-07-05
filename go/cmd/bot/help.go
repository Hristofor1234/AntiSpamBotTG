package main

import (
	"context"
	"fmt"
	"log/slog"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Hristofor1234/AntiSpamBotTG/internal/config"
)

// sendHelp отправляет полное описание функционала бота и текущих настроек.
// Используется и для /start, и для /help — единый текст, чтобы они не
// расходились друг с другом при последующих изменениях.
//
// mode и dbConnected передаются явно, а не вычисляются из cfg внутри
// функции: mode ("Long Polling"/"Webhook") и реальное состояние подключения
// к PostgreSQL (в отличие от того, задан ли просто DATABASE_URL — БД могла
// быть недоступна при старте, см. main.go) известны только вызывающему коду.
func sendHelp(ctx context.Context, b *tgbot.Bot, msg *models.Message, cfg *config.Config, mode string, dbConnected bool, logger *slog.Logger) {
	silentStatus := "выкл (уведомления о банах видны в чате)"
	if cfg.SilentBan {
		silentStatus = "вкл (уведомления о банах не видны в чате)"
	}

	captchaStatus := "выкл"
	if cfg.CaptchaEnabled {
		captchaStatus = fmt.Sprintf("вкл (таймаут %s)", cfg.CaptchaTimeout)
	}

	dbStatus := "❌ не подключена (глобальный ЧС, домены и фильтр слов отключены)"
	if dbConnected {
		dbStatus = "✅ подключена"
	}

	autoDeleteStatus := "выкл"
	if cfg.AutoDeleteDelay > 0 {
		autoDeleteStatus = fmt.Sprintf("вкл (через %s в группах)", cfg.AutoDeleteDelay)
	}

	text := fmt.Sprintf(
		"🛡 *Антиспам-бот — функционал*\n\n"+
			"⚡️ *Автоматическая защита:*\n"+
			"• Антифлуд: %d сообщ. / %s\n"+
			"• Глобальный чёрный список сообщений (хэши, PostgreSQL)\n"+
			"• Проверка ссылок по опасным доменам (PostgreSQL)\n"+
			"• Фильтр по словам чата: %d предупреждений подряд → бан\n"+
			"• Капча для новых участников: %s\n"+
			"• Тихий режим банов: %s\n\n"+
			"👨‍💼 *Команды администраторов чата:*\n"+
			"• /ban (reply на сообщение) — бан + автообучение\n"+
			"• /addspam <слово> — добавить триггер-фразу\n"+
			"• /removespam <слово> — убрать триггер-фразу\n"+
			"• /triggers — список триггер-фраз этого чата\n"+
			"• /addcorewords <категория> — добавить встроенный набор слов (mat, insults, spam, all)\n"+
			"• /blockdomain <домен> — добавить домен в опасные\n"+
			"• /unblockdomain <домен> — убрать домен из опасных\n"+
			"• /domains — последние опасные домены (общий список)\n\n"+
			"👥 *Команды для всех:*\n"+
			"• /report (reply на сообщение) — пожаловаться на спам (порог: %d)\n"+
			"• /start, /help — это сообщение\n\n"+
			"⚙️ *Текущие настройки:*\n"+
			"• Приём обновлений: %s\n"+
			"• Воркеров: %d, очередь: %d\n"+
			"• PostgreSQL: %s\n"+
			"• Автоудаление команд в группах: %s",
		cfg.RateLimitCount, cfg.RateLimitWindow,
		cfg.WarnThreshold,
		captchaStatus,
		silentStatus,
		cfg.ReportThreshold,
		mode, cfg.WorkerCount, cfg.QueueSize,
		dbStatus,
		autoDeleteStatus,
	)

	sendAndScheduleDelete(ctx, b, msg, text, models.ParseModeMarkdownV1, cfg.AutoDeleteDelay, logger)
}
