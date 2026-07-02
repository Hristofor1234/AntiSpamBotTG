// Package config отвечает за загрузку и валидацию конфигурации бота
// из переменных окружения.
package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Config хранит всю конфигурацию приложения, загруженную из окружения.
type Config struct {
	BotToken string

	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	allowedUsers map[string]struct{}
}

// Getenv — минимальный интерфейс над os.Getenv, чтобы Load был тестируемым.
type Getenv func(key string) string

// Load читает конфигурацию из переданной функции получения переменных окружения
// (обычно os.Getenv) и проверяет, что все обязательные значения заданы.
func Load(getenv Getenv) (*Config, error) {
	cfg := &Config{
		BotToken:   strings.TrimSpace(getenv("BOT_TOKEN")),
		DBHost:     strings.TrimSpace(getenv("DB_HOST")),
		DBPort:     strings.TrimSpace(getenv("DB_PORT")),
		DBUser:     strings.TrimSpace(getenv("DB_USER")),
		DBPassword: getenv("DB_PASSWORD"),
		DBName:     strings.TrimSpace(getenv("DB_NAME")),
	}

	if cfg.DBPort == "" {
		cfg.DBPort = "5432"
	}

	var missing []string
	if cfg.BotToken == "" {
		missing = append(missing, "BOT_TOKEN")
	}
	if cfg.DBHost == "" {
		missing = append(missing, "DB_HOST")
	}
	if cfg.DBUser == "" {
		missing = append(missing, "DB_USER")
	}
	if cfg.DBPassword == "" {
		missing = append(missing, "DB_PASSWORD")
	}
	if cfg.DBName == "" {
		missing = append(missing, "DB_NAME")
	}

	allowedRaw := getenv("ALLOWED_USERS")
	if strings.TrimSpace(allowedRaw) == "" {
		missing = append(missing, "ALLOWED_USERS")
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("не заданы обязательные переменные окружения: %s", strings.Join(missing, ", "))
	}

	if _, err := strconv.Atoi(cfg.DBPort); err != nil {
		return nil, fmt.Errorf("DB_PORT должен быть числом, получено %q: %w", cfg.DBPort, err)
	}

	cfg.allowedUsers = make(map[string]struct{})
	for _, u := range strings.Split(allowedRaw, ",") {
		u = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(u), "@")))
		if u != "" {
			cfg.allowedUsers[u] = struct{}{}
		}
	}
	if len(cfg.allowedUsers) == 0 {
		return nil, fmt.Errorf("ALLOWED_USERS указан, но не содержит ни одного username")
	}

	return cfg, nil
}

// IsAllowed проверяет, есть ли username (без учёта регистра и ведущего "@")
// в списке администраторов бота. Реализует интерфейс middleware.AccessChecker.
func (c *Config) IsAllowed(username string) bool {
	username = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(username), "@"))
	if username == "" {
		return false
	}
	_, ok := c.allowedUsers[username]
	return ok
}

// DSN собирает строку подключения к PostgreSQL для pgx, корректно
// экранируя специальные символы в пароле пользователя.
func (c *Config) DSN() string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.DBUser, c.DBPassword),
		Host:   fmt.Sprintf("%s:%s", c.DBHost, c.DBPort),
		Path:   "/" + c.DBName,
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	return u.String()
}
