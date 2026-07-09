package contentfilter

import "testing"

func TestMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "gambling with link",
			text: "Онлайн казино без вложений, переходи по ссылке https://spam-casino.example",
			want: true,
		},
		{
			name: "scam with telegram handle",
			text: "Пассивный доход и крипто сигналы, пиши в лс @super_profit_bot",
			want: true,
		},
		{
			name: "adult spam with reachout",
			text: "18+ знакомства, пиши в личку прямо сейчас",
			want: true,
		},
		{
			name: "normal discussion without spam context",
			text: "Кто может подсказать стратегию ставок ЦБ по инфляции?",
			want: false,
		},
		{
			name: "single suspicious phrase without contact",
			text: "пассивный доход звучит слишком хорошо, чтобы быть правдой",
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, got := Match(tt.text)
			if got != tt.want {
				t.Fatalf("Match(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestMatchWithAllowlist(t *testing.T) {
	t.Parallel()

	text := "Пассивный доход компании, пиши в лс @finance_team"

	if _, matched := Match(text); !matched {
		t.Fatal("Match() = false, want true without allowlist")
	}

	if _, matched := MatchWithAllowlist(text, []string{"пассивный доход компании"}); matched {
		t.Fatal("MatchWithAllowlist() = true, want false when allowlist matches")
	}
}

func TestMatchWithAllowlistNormalizesText(t *testing.T) {
	t.Parallel()

	text := "Пассивный\u200b доход   компании, пиши в лс @finance_team"

	if _, matched := MatchWithAllowlist(text, []string{"  пассивный доход компании  "}); matched {
		t.Fatal("MatchWithAllowlist() = true, want false after normalization")
	}
}
