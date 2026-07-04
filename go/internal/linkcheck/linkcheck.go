// Package linkcheck извлекает домены из ссылок в тексте сообщения — без
// обращения к внешним сервисам (VirusTotal, Google Safe Browsing и т.п.).
// Идея та же, что и у глобального обучения на тексте (см. internal/storage):
// если сообщение с доменом X забанено как спам в одном чате, домен X
// попадает в общий список опасных доменов, и в любом другом чате ссылка на
// него отсекается мгновенно — без ожидания повторного флуда или жалоб.
//
// Сознательно не используется ни один платный/лимитированный внешний API:
// VirusTotal на бесплатном тарифе — 4 запроса в минуту, Google Safe Browsing
// требует отдельный API-ключ. Такой уровень защиты можно добавить поверх
// этого пакета позже, но он не бесплатен и не мгновенен (сетевой вызов на
// каждое сообщение со ссылкой).
package linkcheck

import (
	"net/url"
	"regexp"
	"strings"
)

// commonTLD — ограниченный список доменных зон, которые распознаём и без
// схемы (http/https) и без "www.". Список — компромисс: слишком широкий
// набор (любой ".xx") ловил бы много ложных срабатываний на обычном тексте
// вида "см. пункт 1.2 договора" или сокращения "т.д.", "и.о.".
const tldPattern = `com|net|org|info|biz|xyz|top|club|online|site|shop|website|space|fun|live|vip|pro|icu|cc|tk|ga|cf|gq|ml|link|click|ru|su|рф|kz|ua|by|me|io|co|app`

var urlPattern = regexp.MustCompile(
	`(?i)(?:https?://\S+` +
		`|www\.[a-z0-9-]+(?:\.[a-z0-9-]+)*\.(?:` + tldPattern + `)\S*` +
		`|\b[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*\.(?:` + tldPattern + `)\b\S*)`,
)

// ExtractDomains находит все похожие на ссылки подстроки в text и возвращает
// уникальные домены (в нижнем регистре, без "www."). Возвращает nil, если
// ссылок не найдено.
func ExtractDomains(text string) []string {
	matches := urlPattern.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(matches))
	var domains []string
	for _, m := range matches {
		host := extractHost(m)
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		domains = append(domains, host)
	}
	return domains
}

// extractHost разбирает найденную подстроку как URL (добавляя схему
// "http://", если её нет — иначе net/url не распознает "example.com/x" как
// URL с хостом) и возвращает домен в нижнем регистре без "www.".
func extractHost(raw string) string {
	candidate := raw
	if !strings.Contains(candidate, "://") {
		candidate = "http://" + candidate
	}

	u, err := url.Parse(candidate)
	if err != nil {
		return ""
	}

	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	return host
}
