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

		CREATE TABLE IF NOT EXISTS chat_triggers (
			chat_id    BIGINT NOT NULL,
			phrase     TEXT NOT NULL,
			raw_phrase TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (chat_id, phrase)
		);

		CREATE TABLE IF NOT EXISTS warnings (
			chat_id BIGINT NOT NULL,
			user_id BIGINT NOT NULL,
			count   INT NOT NULL DEFAULT 0,
			PRIMARY KEY (chat_id, user_id)
		);
	`
	if _, err := pool.Exec(ctx, migration); err != nil {
		pool.Close()
		return nil, fmt.Errorf("не удалось применить миграцию: %w", err)
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

// AddTrigger добавляет фразу в список триггер-слов конкретного чата
// (команда /addspam). Фраза хранится в нормализованном виде для сравнения
// (normalizeText) и в исходном — для отображения администраторам
// (ListTriggers). Повторное добавление уже существующей фразы не ошибка.
func (s *Store) AddTrigger(ctx context.Context, chatID int64, phrase string) error {
	normalized := normalizeText(phrase)
	if normalized == "" {
		return fmt.Errorf("пустая фраза после нормализации")
	}

	const query = `
		INSERT INTO chat_triggers (chat_id, phrase, raw_phrase)
		VALUES ($1, $2, $3)
		ON CONFLICT (chat_id, phrase) DO NOTHING
	`
	_, err := s.pool.Exec(ctx, query, chatID, normalized, phrase)
	return err
}

// RemoveTrigger удаляет фразу из списка триггер-слов чата (команда
// /removespam). removed=false означает, что такой фразы и не было.
func (s *Store) RemoveTrigger(ctx context.Context, chatID int64, phrase string) (removed bool, err error) {
	normalized := normalizeText(phrase)
	if normalized == "" {
		return false, nil
	}

	const query = `DELETE FROM chat_triggers WHERE chat_id = $1 AND phrase = $2`
	tag, err := s.pool.Exec(ctx, query, chatID, normalized)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ListTriggers возвращает список триггер-фраз чата в исходном виде (как их
// вводили админы), отсортированный по алфавиту — для команды /triggers.
func (s *Store) ListTriggers(ctx context.Context, chatID int64) ([]string, error) {
	const query = `SELECT raw_phrase FROM chat_triggers WHERE chat_id = $1 ORDER BY raw_phrase`
	rows, err := s.pool.Query(ctx, query, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var phrases []string
	for rows.Next() {
		var phrase string
		if err := rows.Scan(&phrase); err != nil {
			return nil, err
		}
		phrases = append(phrases, phrase)
	}
	return phrases, rows.Err()
}

// MatchTrigger проверяет, содержит ли text (после нормализации) хотя бы
// одну из триггер-фраз чата, и если да — возвращает совпавшую фразу (в
// исходном виде, для логов) и true. Список триггеров чата обычно небольшой
// (десятки фраз), поэтому сравнение делается в Go, а не в SQL: один запрос
// на всё сообщение вместо отдельного запроса на каждую фразу.
func (s *Store) MatchTrigger(ctx context.Context, chatID int64, text string) (phrase string, matched bool, err error) {
	normalizedText := normalizeText(text)
	if normalizedText == "" {
		return "", false, nil
	}

	const query = `SELECT phrase, raw_phrase FROM chat_triggers WHERE chat_id = $1`
	rows, err := s.pool.Query(ctx, query, chatID)
	if err != nil {
		return "", false, err
	}
	defer rows.Close()

	for rows.Next() {
		var normalizedPhrase, rawPhrase string
		if err := rows.Scan(&normalizedPhrase, &rawPhrase); err != nil {
			return "", false, err
		}
		if strings.Contains(normalizedText, normalizedPhrase) {
			return rawPhrase, true, rows.Err()
		}
	}
	return "", false, rows.Err()
}

// AddWarning увеличивает счётчик предупреждений пользователя в чате на 1 и
// возвращает новое значение счётчика (команда-фильтр по словам:
// warnThreshold предупреждений подряд — бан).
func (s *Store) AddWarning(ctx context.Context, chatID, userID int64) (count int, err error) {
	const query = `
		INSERT INTO warnings (chat_id, user_id, count)
		VALUES ($1, $2, 1)
		ON CONFLICT (chat_id, user_id) DO UPDATE SET count = warnings.count + 1
		RETURNING count
	`
	if err := s.pool.QueryRow(ctx, query, chatID, userID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// ResetWarnings обнуляет счётчик предупреждений пользователя в чате —
// вызывается после бана (чтобы не осталось "хвоста" на случай, если тот же
// user_id когда-то future re-join) и может использоваться админами вручную.
func (s *Store) ResetWarnings(ctx context.Context, chatID, userID int64) error {
	const query = `DELETE FROM warnings WHERE chat_id = $1 AND user_id = $2`
	_, err := s.pool.Exec(ctx, query, chatID, userID)
	return err
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

// normalizedHash хэширует нормализованный (см. normalizeText) текст —
// используется для точного (после нормализации) сравнения сообщений в
// global_blacklist.
func normalizedHash(text string) string {
	normalized := normalizeText(text)
	if normalized == "" {
		return ""
	}

	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// normalizeText приводит текст к канонической форме, чтобы ловить не только
// побайтово идентичные копии спама/триггеров, а и их типовые вариации:
// разный регистр, лишние/множественные пробелы, невидимые и управляющие
// юникод-символы — частый приём спамеров, чтобы обойти точное сравнение
// текста, не меняя того, как сообщение выглядит для человека.
func normalizeText(text string) string {
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

	return strings.TrimSpace(b.String())
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
