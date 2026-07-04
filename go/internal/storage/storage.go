// Package storage хранит "выученный" ботом спам в PostgreSQL. Идея —
// глобальное обучение: если сообщение забанено как флуд в одном чате, его
// нормализованный отпечаток сохраняется в global_blacklist, и в любом другом
// чате такой же (с точностью до нормализации) спам отсекается мгновенно —
// на "нулевом" уровне обороны, ещё до срабатывания rate-limit.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5/pgxpool"
)

// pingMaxAttempts/pingRetryDelay — сколько раз и с какой паузой пробуем
// достучаться до PostgreSQL при старте. Нужно в первую очередь для
// docker-compose: контейнер bot нередко стартует раньше, чем db успевает
// принять соединения.
const (
	pingMaxAttempts = 6
	pingRetryDelay  = 2 * time.Second

	sampleMaxLen = 300
)

// Store — обёртка над пулом соединений PostgreSQL для хранения глобального
// чёрного списка спама.
type Store struct {
	pool *pgxpool.Pool
}

// New открывает пул соединений по dsn, дожидается доступности БД (с
// повторами) и применяет миграцию. Миграция идемпотентна — вызывать New
// при каждом старте безопасно.
func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать пул соединений с PostgreSQL: %w", err)
	}

	if err := pingWithRetry(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}

	const migration = `
		CREATE TABLE IF NOT EXISTS global_blacklist (
			hash        VARCHAR(64) PRIMARY KEY,
			sample_text TEXT NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`
	if _, err := pool.Exec(ctx, migration); err != nil {
		pool.Close()
		return nil, fmt.Errorf("не удалось применить миграцию global_blacklist: %w", err)
	}

	return &Store{pool: pool}, nil
}

// Close закрывает пул соединений. Безопасно вызывать даже если New вернул
// ошибку до этого — pool.Close идемпотентен для уже созданного пула.
func (s *Store) Close() {
	s.pool.Close()
}

// LearnSpam добавляет текст в глобальный чёрный список ("обучает" бота):
// в любом чате сообщение с таким же (после нормализации) текстом будет
// опознано как спам через IsKnownSpam мгновенно, без ожидания rate-limit.
func (s *Store) LearnSpam(ctx context.Context, text string) error {
	hash := normalizedHash(text)
	if hash == "" {
		return nil // после нормализации ничего не осталось — учить нечему
	}

	sample := text
	if len(sample) > sampleMaxLen {
		sample = sample[:sampleMaxLen]
	}

	const query = `
		INSERT INTO global_blacklist (hash, sample_text)
		VALUES ($1, $2)
		ON CONFLICT (hash) DO NOTHING
	`
	_, err := s.pool.Exec(ctx, query, hash, sample)
	return err
}

// IsKnownSpam проверяет, встречался ли уже (после нормализации) такой текст
// в глобальном чёрном списке — неважно, в каком чате его выучили.
func (s *Store) IsKnownSpam(ctx context.Context, text string) (bool, error) {
	hash := normalizedHash(text)
	if hash == "" {
		return false, nil
	}

	var exists bool
	const query = `SELECT EXISTS(SELECT 1 FROM global_blacklist WHERE hash = $1)`
	if err := s.pool.QueryRow(ctx, query, hash).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// pingWithRetry повторяет Ping с паузой, пока PostgreSQL не станет доступен
// или не закончатся попытки/не отменится ctx.
func pingWithRetry(ctx context.Context, pool *pgxpool.Pool) error {
	var lastErr error

	for attempt := 1; attempt <= pingMaxAttempts; attempt++ {
		err := pool.Ping(ctx)
		if err == nil {
			return nil
		}
		lastErr = err

		select {
		case <-ctx.Done():
			return fmt.Errorf("PostgreSQL недоступен: %w", ctx.Err())
		case <-time.After(pingRetryDelay):
		}
	}

	return fmt.Errorf("PostgreSQL недоступен после %d попыток: %w", pingMaxAttempts, lastErr)
}

// normalizedHash приводит текст к канонической форме перед хэшированием,
// чтобы ловить не только побайтово идентичные копии спама, а и его типовые
// вариации: разный регистр, лишние/множественные пробелы, невидимые и
// управляющие юникод-символы — частый приём спамеров, чтобы обойти точное
// сравнение текста, не меняя того, как сообщение выглядит для человека.
func normalizedHash(text string) string {
	var b strings.Builder
	lastWasSpace := true // чтобы обрезать пробелы в начале строки

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

	normalized := strings.TrimSpace(b.String())
	if normalized == "" {
		return ""
	}

	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// isInvisible возвращает true для символов нулевой ширины и форматирования,
// которые спамеры вставляют между буквами, чтобы обойти простое сравнение
// текста.
func isInvisible(r rune) bool {
	switch r {
	case rune(0x200B), // zero width space
		rune(0x200C), // zero width non-joiner
		rune(0x200D), // zero width joiner
		rune(0xFEFF), // BOM / zero width no-break space
		rune(0x2060): // word joiner
		return true
	default:
		return false
	}
}
