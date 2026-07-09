package main

import (
	"strings"

	"github.com/go-telegram/bot/models"
)

// moderationMessageFromUpdate возвращает сообщение из update, которое нужно
// прогонять через антиспам-конвейер. Помимо новых сообщений учитываем и
// редактирования: спамер может сначала отправить "безобидный" текст, а
// затем отредактировать его в рекламу/скам.
func moderationMessageFromUpdate(update *models.Update) *models.Message {
	if update == nil {
		return nil
	}
	if update.Message != nil {
		return update.Message
	}
	return update.EditedMessage
}

// messageModerationText возвращает содержимое сообщения, которое имеет смысл
// прогонять через антиспам-фильтры и сохранять в обучение. В Telegram спам
// часто приходит не только обычным текстом, но и как подпись к фото/видео/
// документу, поэтому caption учитывается наравне с text.
func messageModerationText(msg *models.Message) string {
	if msg == nil {
		return ""
	}

	text := strings.TrimSpace(msg.Text)
	caption := strings.TrimSpace(msg.Caption)

	switch {
	case text == "":
		return caption
	case caption == "":
		return text
	default:
		return text + "\n" + caption
	}
}
