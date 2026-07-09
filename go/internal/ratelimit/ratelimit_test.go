package ratelimit

import (
	"testing"
	"time"
)

func TestAllowIsPerChat(t *testing.T) {
	t.Parallel()

	limiter := New(2, time.Minute)

	if !limiter.Allow(100, 1) {
		t.Fatal("first message in chat 100 should be allowed")
	}
	if !limiter.Allow(100, 1) {
		t.Fatal("second message in chat 100 should be allowed")
	}
	if limiter.Allow(100, 1) {
		t.Fatal("third message in chat 100 should exceed limit")
	}

	// В другом чате тот же пользователь не должен быть "заражён"
	// лимитом предыдущего чата.
	if !limiter.Allow(200, 1) {
		t.Fatal("first message in chat 200 should be allowed independently")
	}
}
