package main

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestMessageModerationText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  *models.Message
		want string
	}{
		{
			name: "text only",
			msg:  &models.Message{Text: "spam text"},
			want: "spam text",
		},
		{
			name: "caption only",
			msg:  &models.Message{Caption: "spam caption"},
			want: "spam caption",
		},
		{
			name: "text and caption",
			msg:  &models.Message{Text: "text", Caption: "caption"},
			want: "text\ncaption",
		},
		{
			name: "trim spaces",
			msg:  &models.Message{Text: "  text  ", Caption: "  caption  "},
			want: "text\ncaption",
		},
		{
			name: "nil message",
			msg:  nil,
			want: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := messageModerationText(tt.msg); got != tt.want {
				t.Fatalf("messageModerationText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestModerationMessageFromUpdate(t *testing.T) {
	t.Parallel()

	msg := &models.Message{ID: 1}
	edited := &models.Message{ID: 2}

	if got := moderationMessageFromUpdate(&models.Update{Message: msg}); got != msg {
		t.Fatal("expected original message to be returned")
	}
	if got := moderationMessageFromUpdate(&models.Update{EditedMessage: edited}); got != edited {
		t.Fatal("expected edited message to be returned")
	}
	if got := moderationMessageFromUpdate(nil); got != nil {
		t.Fatal("expected nil for nil update")
	}
}
