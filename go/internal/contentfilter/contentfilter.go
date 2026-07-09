// Package contentfilter содержит встроенные эвристики для автоудаления
// очевидного спама и сообщений с плохим контекстом без ручного наполнения
// триггер-слов конкретного чата. Правила здесь специально достаточно
// строгие: бан срабатывает только на комбинации "подозрительная тема" +
// "призыв к контакту/ссылка/контактный хэндл", чтобы уменьшить ложные
// срабатывания на обычную переписку.
package contentfilter

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/Hristofor1234/AntiSpamBotTG/internal/linkcheck"
)

var (
	telegramHandlePattern = regexp.MustCompile(`(?i)(?:^|[\s(])@[a-z0-9_]{5,}`)
	telegramLinkPattern   = regexp.MustCompile(`(?i)(?:t\.me/|telegram\.me/)`)
)

var adultKeywords = []string{
	"интим", "18+", "секс", "порно", "эскорт", "вебкам", "onlyfans",
}

var gamblingKeywords = []string{
	"казино", "ставки", "слоты", "беттинг", "букмекер", "выигрыш без вложений",
	"обыграть казино", "беспроигрышная стратегия",
}

var scamKeywords = []string{
	"заработок без вложений", "доход без вложений", "лёгкий заработок",
	"легкий заработок", "пассивный доход", "раскрутка счёта",
	"гарантированный доход", "крипто сигналы", "арбитраж криптовалют",
	"торговый бот", "секретная стратегия", "инсайд от аналитиков",
	"закрытый канал", "закрытый клуб", "забери подарок", "забирай гайд",
	"бесплатная консультация",
}

var callToActionKeywords = []string{
	"пиши в лс", "пиши в личку", "подробности в лс", "жми на профиль",
	"переходи по ссылке", "в личные сообщения", "в лс", "в директ",
}

var moneyKeywords = []string{
	"заработок", "доход", "деньги", "прибыль", "крипто", "сигналы",
	"инвестиции", "ставки", "казино", "выигрыш",
}

// Match возвращает причину и true, если текст очень похож на спам/скам с
// плохим контекстом: подозрительная тема подкреплена ссылкой, контактом или
// призывом увести пользователя в личку/по внешней ссылке.
func Match(text string) (reason string, matched bool) {
	return MatchWithAllowlist(text, nil)
}

// MatchWithAllowlist делает то же, что Match, но сначала проверяет
// allowlistSubstrings: если после нормализации сообщение содержит хотя бы
// одну разрешённую подстроку, встроенный фильтр его не трогает. Это нужно
// для чатов с легитимными сообщениями, которые по форме похожи на "плохой
// контекст" и иначе регулярно попадали бы под эвристику.
func MatchWithAllowlist(text string, allowlistSubstrings []string) (reason string, matched bool) {
	normalized := normalizeText(text)
	if normalized == "" {
		return "", false
	}
	if containsNormalizedAny(normalized, allowlistSubstrings) {
		return "", false
	}

	hasDomain := len(linkcheck.ExtractDomains(normalized)) > 0
	hasTelegramLink := telegramLinkPattern.MatchString(normalized)
	hasHandle := telegramHandlePattern.MatchString(normalized)
	hasContact := hasTelegramLink || hasHandle
	hasReachout := containsAny(normalized, callToActionKeywords)

	switch {
	case containsAny(normalized, adultKeywords) && (hasDomain || hasContact || hasReachout):
		return "adult-spam", true
	case containsAny(normalized, gamblingKeywords) && (hasDomain || hasContact || hasReachout):
		return "gambling-spam", true
	case containsAny(normalized, scamKeywords) && (hasDomain || hasContact || hasReachout):
		return "scam-spam", true
	case containsAny(normalized, moneyKeywords) && hasReachout && (hasDomain || hasContact):
		return "lead-generation-spam", true
	default:
		return "", false
	}
}

func containsAny(text string, keywords []string) bool {
	return containsNormalizedAny(text, keywords)
}

func containsNormalizedAny(text string, keywords []string) bool {
	for _, keyword := range keywords {
		normalizedKeyword := normalizeText(keyword)
		if normalizedKeyword != "" && strings.Contains(text, normalizedKeyword) {
			return true
		}
	}
	return false
}

func normalizeText(text string) string {
	var b strings.Builder
	lastWasSpace := true

	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsControl(r) || isInvisible(r):
			continue
		case unicode.IsSpace(r):
			if !lastWasSpace {
				b.WriteRune(' ')
				lastWasSpace = true
			}
		default:
			b.WriteRune(r)
			lastWasSpace = false
		}
	}

	return strings.TrimSpace(b.String())
}

func isInvisible(r rune) bool {
	switch r {
	case rune(0x200B),
		rune(0x200C),
		rune(0x200D),
		rune(0xFEFF),
		rune(0x2060):
		return true
	default:
		return false
	}
}
