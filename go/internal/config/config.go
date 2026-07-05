// Package config отвечает за загрузку и валидацию конфигурации бота
// из переменных окружения.
package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Config хранит всю конфигурацию приложения, загруженную из окружения.
type Config struct {
	BotToken string

	// Антифлуд: не более RateLimitCount сообщений за RateLimitWindow.
	RateLimitCount  int
	RateLimitWindow time.Duration

	// Пул воркеров, обрабатывающих очередь обновлений.
	WorkerCount int
	QueueSize   int

	// Webhook: если WebhookURL задан, бот принимает обновления через
	// HTTP-сервер (setWebhook) вместо long polling. WebhookURL — это
	// публичный HTTPS-адрес (например, https://example.com/webhook/<путь>),
	// на который Telegram будет слать апдейты; обычно это TLS-домен с
	// reverse-proxy (nginx/Caddy) перед WebhookListenAddr. Если WebhookURL
	// пуст — используется Long Polling (b.Start), поведение не меняется.
	WebhookURL         string
	WebhookSecretToken string
	WebhookListenAddr  string
	WebhookPath        string

	// DatabaseURL — DSN PostgreSQL для глобального обучения (общий чёрный
	// список спама между всеми чатами, где стоит бот). Необязательный: если
	// пуст, бот работает только на in-memory rate-limit, как раньше. Если
	// PostgreSQL окажется недоступен при старте, бот тоже не падает — просто
	// продолжает без глобального обучения (см. main.go).
	DatabaseURL string

	// ReportThreshold — сколько разных пользователей должны пожаловаться
	// (команда /report в ответ на сообщение), чтобы бот забанил автора и
	// добавил текст в глобальное обучение.
	ReportThreshold int

	// SilentBan — если true, бот не пишет в чат уведомление о бане
	// ("🚫 Пользователь X забанен..."). Удаление сообщения, бан и обучение
	// при этом происходят как обычно — меняется только видимость для чата.
	SilentBan bool

	// WarnThreshold — сколько раз подряд участник может написать сообщение
	// с триггер-словом чата (см. /addspam), прежде чем будет забанен вместо
	// очередного предупреждения. Требует подключённой PostgreSQL — без неё
	// фильтр по словам отключён (см. main.go).
	WarnThreshold int

	// CaptchaEnabled/CaptchaTimeout — проверка "не бот" для новых участников
	// чата: сразу после вступления пользователь получает ограничение на
	// отправку сообщений и кнопку "Я не бот"; не подтвердил за CaptchaTimeout
	// — считается ботом и удаляется из чата (кик, не постоянный бан).
	CaptchaEnabled bool
	CaptchaTimeout time.Duration

	// AutoDeleteDelay — через сколько после отправки удалять команду
	// администратора и ответ бота в групповых чатах (не в личных — там
	// удаление мешало бы обычной переписке). 0 отключает автоудаление.
	AutoDeleteDelay time.Duration
}

// Getenv — минимальный интерфейс над os.Getenv, чтобы Load был тестируемым.
type Getenv func(key string) string

// Load читает конфигурацию из переданной функции получения переменных
// окружения (обычно os.Getenv) и проверяет обязательные значения.
func Load(getenv Getenv) (*Config, error) {
	token := strings.TrimSpace(getenv("BOT_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("не задана обязательная переменная окружения: BOT_TOKEN")
	}

	rateLimitCount, err := intEnv(getenv, "RATE_LIMIT_COUNT", 5)
	if err != nil {
		return nil, err
	}
	if rateLimitCount <= 0 {
		return nil, fmt.Errorf("RATE_LIMIT_COUNT должен быть положительным числом, получено %d", rateLimitCount)
	}

	rateLimitWindowSec, err := intEnv(getenv, "RATE_LIMIT_WINDOW_SECONDS", 3)
	if err != nil {
		return nil, err
	}
	if rateLimitWindowSec <= 0 {
		return nil, fmt.Errorf("RATE_LIMIT_WINDOW_SECONDS должен быть положительным числом, получено %d", rateLimitWindowSec)
	}

	workerCount, err := intEnv(getenv, "WORKER_COUNT", 10)
	if err != nil {
		return nil, err
	}
	if workerCount <= 0 {
		return nil, fmt.Errorf("WORKER_COUNT должен быть положительным числом, получено %d", workerCount)
	}

	queueSize, err := intEnv(getenv, "QUEUE_SIZE", 5000)
	if err != nil {
		return nil, err
	}
	if queueSize <= 0 {
		return nil, fmt.Errorf("QUEUE_SIZE должен быть положительным числом, получено %d", queueSize)
	}

	webhookURL := strings.TrimSpace(getenv("WEBHOOK_URL"))
	if webhookURL != "" && !strings.HasPrefix(webhookURL, "https://") {
		return nil, fmt.Errorf("WEBHOOK_URL должен начинаться с https:// (требование Telegram), получено %q", webhookURL)
	}

	webhookSecretToken := strings.TrimSpace(getenv("WEBHOOK_SECRET_TOKEN"))
	if webhookURL != "" && webhookSecretToken == "" {
		return nil, fmt.Errorf("при заданном WEBHOOK_URL обязательна WEBHOOK_SECRET_TOKEN (защита от поддельных запросов на вебхук)")
	}

	webhookListenAddr := strings.TrimSpace(getenv("WEBHOOK_LISTEN_ADDR"))
	if webhookListenAddr == "" {
		webhookListenAddr = ":8080"
	}

	webhookPath := strings.TrimSpace(getenv("WEBHOOK_PATH"))
	if webhookPath == "" {
		webhookPath = "/webhook"
	}
	if !strings.HasPrefix(webhookPath, "/") {
		webhookPath = "/" + webhookPath
	}

	databaseURL := strings.TrimSpace(getenv("DATABASE_URL"))

	reportThreshold, err := intEnv(getenv, "REPORT_THRESHOLD", 3)
	if err != nil {
		return nil, err
	}
	if reportThreshold < 2 {
		return nil, fmt.Errorf("REPORT_THRESHOLD должен быть не меньше 2 (иначе теряется смысл коллективной проверки), получено %d", reportThreshold)
	}

	silentBan, err := boolEnv(getenv, "SILENT_BAN", false)
	if err != nil {
		return nil, err
	}

	warnThreshold, err := intEnv(getenv, "WARN_THRESHOLD", 3)
	if err != nil {
		return nil, err
	}
	if warnThreshold < 1 {
		return nil, fmt.Errorf("WARN_THRESHOLD должен быть не меньше 1, получено %d", warnThreshold)
	}

	captchaEnabled, err := boolEnv(getenv, "CAPTCHA_ENABLED", true)
	if err != nil {
		return nil, err
	}

	captchaTimeoutSec, err := intEnv(getenv, "CAPTCHA_TIMEOUT_SECONDS", 120)
	if err != nil {
		return nil, err
	}
	if captchaTimeoutSec <= 0 {
		return nil, fmt.Errorf("CAPTCHA_TIMEOUT_SECONDS должен быть положительным числом, получено %d", captchaTimeoutSec)
	}

	autoDeleteDelaySec, err := intEnv(getenv, "AUTO_DELETE_DELAY_SECONDS", 30)
	if err != nil {
		return nil, err
	}
	if autoDeleteDelaySec < 0 {
		return nil, fmt.Errorf("AUTO_DELETE_DELAY_SECONDS не может быть отрицательным, получено %d", autoDeleteDelaySec)
	}

	return &Config{
		BotToken:        token,
		RateLimitCount:  rateLimitCount,
		RateLimitWindow: time.Duration(rateLimitWindowSec) * time.Second,
		WorkerCount:     workerCount,
		QueueSize:       queueSize,

		WebhookURL:         webhookURL,
		WebhookSecretToken: webhookSecretToken,
		WebhookListenAddr:  webhookListenAddr,
		WebhookPath:        webhookPath,

		DatabaseURL: databaseURL,

		ReportThreshold: reportThreshold,

		SilentBan:     silentBan,
		WarnThreshold: warnThreshold,

		CaptchaEnabled: captchaEnabled,
		CaptchaTimeout: time.Duration(captchaTimeoutSec) * time.Second,

		AutoDeleteDelay: time.Duration(autoDeleteDelaySec) * time.Second,
	}, nil
}

// intEnv читает переменную окружения как int; если она не задана —
// возвращает def. Ошибка возвращается только если значение задано, но
// не является числом.
func intEnv(getenv Getenv, key string, def int) (int, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return def, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s должен быть целым числом, получено %q: %w", key, raw, err)
	}
	return v, nil
}

// boolEnv читает переменную окружения как bool (true/false/1/0/...); если
// она не задана — возвращает def.
func boolEnv(getenv Getenv, key string, def bool) (bool, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return def, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s должен быть true/false, получено %q: %w", key, raw, err)
	}
	return v, nil
}
