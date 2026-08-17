package handler

import "testing"

func TestNormalizeExecutionLimit(t *testing.T) {
	t.Run("empty uses default limit", func(t *testing.T) {
		if got := normalizeExecutionLimit("", 50, 50000); got != 50 {
			t.Fatalf("expected default limit 50, got %d", got)
		}
	})

	t.Run("valid values are kept within max", func(t *testing.T) {
		if got := normalizeExecutionLimit("250", 50, 50000); got != 250 {
			t.Fatalf("expected limit 250, got %d", got)
		}
	})

	t.Run("values above max are clamped", func(t *testing.T) {
		if got := normalizeExecutionLimit("70000", 50, 50000); got != 50000 {
			t.Fatalf("expected clamped limit 50000, got %d", got)
		}
	})

	t.Run("invalid values fall back to default", func(t *testing.T) {
		if got := normalizeExecutionLimit("abc", 50, 50000); got != 50 {
			t.Fatalf("expected default limit 50, got %d", got)
		}
	})
}
