package handler

import (
	"strings"
	"testing"
)

func TestNormalizeAvatarURL(t *testing.T) {
	for _, value := range []string{"", "  ", "https://example.com/photo.png", " https://example.com/photo.jpg?version=2 "} {
		got, err := normalizeAvatarURL(value)
		if err != nil || got != strings.TrimSpace(value) {
			t.Fatalf("valid URL rejected: %q %v", value, err)
		}
	}
	for _, value := range []string{"http://example.com/photo.png", "javascript:alert(1)", "data:image/png;base64,abc", "//example.com/photo.png", "https://", "https://user:pass@example.com/photo.png", "https://example.com/" + strings.Repeat("x", 2048), "https://example.com/\nimage"} {
		if _, err := normalizeAvatarURL(value); err == nil {
			t.Errorf("invalid URL accepted: %q", value)
		}
	}
}
