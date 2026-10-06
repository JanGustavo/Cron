package httputil

import (
	"errors"
	"io"
	"strings"
	"testing"
	"unicode/utf8"
)

type brokenReader struct{}

func (brokenReader) Read(p []byte) (int, error) {
	return copy(p, `{"success":true}`), errors.New("connection closed")
}

func TestCaptureLimits(t *testing.T) {
	for _, size := range []int{0, 4096, MaxEvaluationBodySize, MaxEvaluationBodySize + 1} {
		body, reason := readResponseBody(strings.NewReader(strings.Repeat("x", size)))
		if size <= MaxEvaluationBodySize {
			if len(body) != size || reason != "" {
				t.Fatalf("complete body lost: %d, %s", len(body), reason)
			}
		} else if len(body) != MaxEvaluationBodySize || reason != "body_too_large" {
			t.Fatalf("oversize silently accepted: %d, %s", len(body), reason)
		}
	}
	body, reason := readResponseBody(brokenReader{})
	if len(body) == 0 || reason != "body_read_error" {
		t.Fatal("partial read error was hidden")
	}
	_, reason = readResponseBody(io.LimitReader(strings.NewReader("ok"), 2))
	if reason != "" {
		t.Fatal("exact EOF treated as failure")
	}
}

func TestLogPreviewDoesNotLimitEvaluationBody(t *testing.T) {
	body := strings.Repeat("a", 2047) + "é" + strings.Repeat("z", 2048)
	preview := ResponsePreview(body)
	if len(preview) > 2048 || !utf8.ValidString(preview) || len(body) <= 2048 {
		t.Fatal("invalid preview or modified capture")
	}
	if !utf8.ValidString(ResponsePreview("ok\xff")) {
		t.Fatal("invalid source bytes leaked into preview")
	}
	if ResponsePreview("ok") != "ok" {
		t.Fatal("short preview changed")
	}
}
