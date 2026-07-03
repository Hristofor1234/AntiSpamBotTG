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

	return &Config{
		BotToken:        token,
		RateLimitCount:  rateLimitCount,
		RateLimitWindow: time.Duration(rateLimitWindowSec) * time.Second,
		WorkerCount:     workerCount,
		QueueSize:       queueSize,
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
