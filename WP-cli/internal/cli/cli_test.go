package cli

import (
	"testing"

	"wp/internal/store"
)

func TestFormatReactions(t *testing.T) {
	if got := formatReactions(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
	got := formatReactions([]store.Reaction{
		{Emoji: "👍"},
		{Emoji: "👍"},
		{Emoji: "❤️", FromMe: true},
	})
	if got != "👍×2  ❤️*" {
		t.Fatalf("got %q", got)
	}
}
