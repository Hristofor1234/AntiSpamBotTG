// Package database отвечает за подключение к PostgreSQL, хранение
// спам-слов и поддержание их копии в памяти (кэш) для быстрой проверки
// сообщений без обращения к БД на каждое сообщение.
package database

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB — обёртка над пулом соединений pgx с потокобезопасным
// in-memory кэшем спам-слов.
type DB struct {
	pool *pgxpool.Pool

	mu    sync.RWMutex
	cache map[string]struct{}
}

// New создаёт пул подключений к PostgreSQL и проверяет его доступность (ping).
func New(ctx context.Context, dsn string) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("не удалось разобрать строку подключения: %w", err)
	}

	poolCfg.MinConns = 5
	poolCfg.MaxConns = 10
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.MaxConnIdleTime = 30 * time.Minute
	poolCfg.HealthCheckPeriod = time.Minute
	poolCfg.ConnConfig.ConnectTimeout = 10 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать пул подключений: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("не удалось подключиться к БД: %w", err)
	}

	return &DB{
		pool:  pool,
		cache: make(map[string]struct{}),
	}, nil
}

// Close закрывает пул подключений.
func (d *DB) Close() {
	d.pool.Close()
}

// InitSchema создаёт таблицу spam_keywords, если она ещё не существует.
func (d *DB) InitSchema(ctx context.Context) error {
	_, err := d.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS spam_keywords (
			id SERIAL PRIMARY KEY,
			word VARCHAR(255) UNIQUE NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("не удалось создать таблицу spam_keywords: %w", err)
	}
	return nil
}

// LoadCache загружает все слова из БД в кэш (используется при старте бота).
// Возвращает количество загруженных слов.
func (d *DB) LoadCache(ctx context.Context) (int, error) {
	rows, err := d.pool.Query(ctx, "SELECT word FROM spam_keywords")
	if err != nil {
		return 0, fmt.Errorf("не удалось загрузить ключевые слова: %w", err)
	}
	defer rows.Close()

	newCache := make(map[string]struct{})
	for rows.Next() {
		var word string
		if err := rows.Scan(&word); err != nil {
			return 0, fmt.Errorf("не удалось прочитать строку результата: %w", err)
		}
		newCache[word] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	d.mu.Lock()
	d.cache = newCache
	d.mu.Unlock()

	return len(newCache), nil
}

// AddWords добавляет слова в БД и обновляет кэш в памяти.
// Возвращает только реально добавленные (новые, не дублирующиеся) слова.
func (d *DB) AddWords(ctx context.Context, words []string) ([]string, error) {
	normalized := normalizeAll(words)
	if len(normalized) == 0 {
		return nil, nil
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("не удалось начать транзакцию: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var added []string
	for _, w := range normalized {
		tag, err := tx.Exec(ctx, "INSERT INTO spam_keywords (word) VALUES ($1) ON CONFLICT DO NOTHING", w)
		if err != nil {
			return nil, fmt.Errorf("не удалось добавить слово %q: %w", w, err)
		}
		if tag.RowsAffected() > 0 {
			added = append(added, w)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("не удалось зафиксировать транзакцию: %w", err)
	}

	if len(added) > 0 {
		d.mu.Lock()
		for _, w := range added {
			d.cache[w] = struct{}{}
		}
		d.mu.Unlock()
	}

	return added, nil
}

// RemoveWords удаляет слова из БД и обновляет кэш в памяти.
// Возвращает только реально удалённые (существовавшие) слова.
func (d *DB) RemoveWords(ctx context.Context, words []string) ([]string, error) {
	normalized := normalizeAll(words)
	if len(normalized) == 0 {
		return nil, nil
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("не удалось начать транзакцию: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var removed []string
	for _, w := range normalized {
		tag, err := tx.Exec(ctx, "DELETE FROM spam_keywords WHERE word = $1", w)
		if err != nil {
			return nil, fmt.Errorf("не удалось удалить слово %q: %w", w, err)
		}
		if tag.RowsAffected() > 0 {
			removed = append(removed, w)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("не удалось зафиксировать транзакцию: %w", err)
	}

	if len(removed) > 0 {
		d.mu.Lock()
		for _, w := range removed {
			delete(d.cache, w)
		}
		d.mu.Unlock()
	}

	return removed, nil
}

// ListWords возвращает список всех слов из кэша (без обращения к БД).
func (d *DB) ListWords() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	words := make([]string, 0, len(d.cache))
	for w := range d.cache {
		words = append(words, w)
	}
	return words
}

// ContainsSpam регистронезависимо проверяет, встречается ли в тексте
// хотя бы одно слово из кэша спам-слов. Возвращает найденное слово
// и признак совпадения. Обращение только к кэшу в памяти — без запроса к БД.
func (d *DB) ContainsSpam(text string) (string, bool) {
	text = strings.ToLower(text)

	d.mu.RLock()
	defer d.mu.RUnlock()

	for word := range d.cache {
		if strings.Contains(text, word) {
			return word, true
		}
	}
	return "", false
}

func normalizeAll(words []string) []string {
	seen := make(map[string]struct{}, len(words))
	result := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.ToLower(strings.TrimSpace(w))
		if w == "" {
			continue
		}
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		result = append(result, w)
	}
	return result
}
