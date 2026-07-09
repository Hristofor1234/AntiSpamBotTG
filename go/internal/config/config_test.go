package config

import "testing"

func TestLoadContentFilterAction(t *testing.T) {
	t.Parallel()

	base := func(extra map[string]string) Getenv {
		values := map[string]string{
			"BOT_TOKEN": "token",
		}
		for k, v := range extra {
			values[k] = v
		}
		return func(key string) string {
			return values[key]
		}
	}

	t.Run("default action is ban", func(t *testing.T) {
		t.Parallel()

		cfg, err := Load(base(nil))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.ContentFilterAction != "ban" {
			t.Fatalf("ContentFilterAction = %q, want ban", cfg.ContentFilterAction)
		}
	})

	t.Run("accept delete action", func(t *testing.T) {
		t.Parallel()

		cfg, err := Load(base(map[string]string{
			"CONTENT_FILTER_ACTION": "delete",
		}))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.ContentFilterAction != "delete" {
			t.Fatalf("ContentFilterAction = %q, want delete", cfg.ContentFilterAction)
		}
	})

	t.Run("reject invalid action", func(t *testing.T) {
		t.Parallel()

		_, err := Load(base(map[string]string{
			"CONTENT_FILTER_ACTION": "warn",
		}))
		if err == nil {
			t.Fatal("Load() error = nil, want validation error")
		}
	})

	t.Run("parse allowlist substrings", func(t *testing.T) {
		t.Parallel()

		cfg, err := Load(base(map[string]string{
			"CONTENT_FILTER_ALLOW_SUBSTRINGS": "ставок ЦБ, пассивный доход компании , , пиши в лс менеджеру",
		}))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if len(cfg.ContentFilterAllowSubstrings) != 3 {
			t.Fatalf("len(ContentFilterAllowSubstrings) = %d, want 3", len(cfg.ContentFilterAllowSubstrings))
		}
	})
}
