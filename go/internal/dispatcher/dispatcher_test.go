package dispatcher

import "testing"

func TestApplyContentFilterMode(t *testing.T) {
	t.Parallel()

	base := contentFilterSettings{
		Enabled:         true,
		Action:          "ban",
		AllowSubstrings: []string{"global"},
	}

	tests := []struct {
		name string
		mode string
		want contentFilterSettings
	}{
		{
			name: "default mode keeps settings",
			mode: "",
			want: base,
		},
		{
			name: "off disables filter",
			mode: "off",
			want: contentFilterSettings{
				Enabled:         false,
				Action:          "ban",
				AllowSubstrings: []string{"global"},
			},
		},
		{
			name: "delete overrides action",
			mode: "delete",
			want: contentFilterSettings{
				Enabled:         true,
				Action:          "delete",
				AllowSubstrings: []string{"global"},
			},
		},
		{
			name: "ban keeps ban action",
			mode: "ban",
			want: base,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := applyContentFilterMode(base, tt.mode)
			if got.Enabled != tt.want.Enabled || got.Action != tt.want.Action {
				t.Fatalf("applyContentFilterMode(%q) = %+v, want %+v", tt.mode, got, tt.want)
			}
			if len(got.AllowSubstrings) != len(tt.want.AllowSubstrings) {
				t.Fatalf("allowlist length = %d, want %d", len(got.AllowSubstrings), len(tt.want.AllowSubstrings))
			}
		})
	}
}

func TestAppendContentFilterAllowlist(t *testing.T) {
	t.Parallel()

	base := contentFilterSettings{
		Enabled:         true,
		Action:          "ban",
		AllowSubstrings: []string{"global-1", "global-2"},
	}

	got := appendContentFilterAllowlist(base, []string{"chat-1", "chat-2"})
	if len(got.AllowSubstrings) != 4 {
		t.Fatalf("len(AllowSubstrings) = %d, want 4", len(got.AllowSubstrings))
	}
	if got.AllowSubstrings[2] != "chat-1" || got.AllowSubstrings[3] != "chat-2" {
		t.Fatalf("unexpected merged allowlist: %#v", got.AllowSubstrings)
	}
}
