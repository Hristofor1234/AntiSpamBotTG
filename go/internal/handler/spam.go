package handler

import (
	"context"
	"log/slog"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Hristofor1234/AntiSpamBotTG/internal/database"
)

// maxLogPreviewRunes — сколько символов текста сообщения попадает в лог
// при удалении спам-сообщения.
const maxLogPreviewRunes = 50

// Spam — обработчик по умолчанию, фильтрующий все входящие текстовые
// сообщения (кроме зарегистрированных команд) на наличие спам-слов.
type Spam struct {
	db     *database.DB
	logger *slog.Logger
}

// NewSpam создаёт обработчик фильтра спама.
func NewSpam(db *database.DB, logger *slog.Logger) *Spam {
	return &Spam{db: db, logger: logger}
}

// Filter проверяет текст сообщения на спам-слова (регистронезависимо,
// по кэшу в памяти — без запроса к БД) и мгновенно удаляет сообщение
// при совпадении. Сообщения от бота самого себя и от других ботов
// игнорируются.
func (s *Spam) Filter(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil || msg.Text == "" {
		return
	}

	from := msg.From
	if from == nil || from.IsBot {
		return
	}

	word, isSpam := s.db.ContainsSpam(msg.Text)
	if !isSpam {
		return
	}

	_, err := b.DeleteMessage(ctx, &bot.DeleteMessageParams{
		ChatID:    msg.Chat.ID,
		MessageID: msg.ID,
	})
	if err != nil {
		s.logger.Error("не удалось удалить сообщение",
			"error", err, "chat_id", msg.Chat.ID, "message_id", msg.ID)
		return
	}

	s.logger.Info("удалено спам-сообщение",
		"username", from.Username,
		"matched_word", word,
		"text_preview", previewOf(msg.Text))
}

// previewOf безопасно (по рунам, а не байтам) обрезает текст до
// maxLogPreviewRunes символов для логирования.
func previewOf(text string) string {
	runes := []rune(text)
	if len(runes) <= maxLogPreviewRunes {
		return text
	}
	return string(runes[:maxLogPreviewRunes])
}
