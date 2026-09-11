package service

import (
	"testing"

	"github.com/JanGustavo/Cron/internal/domain/monitor"
)

func TestExtractJSONFieldNested(t *testing.T) {
	data := map[string]any{
		"status": "ok",
		"data": map[string]any{
			"synced": true,
			"accountsOnline": float64(1200),
		},
	}

	cases := []struct {
		name string
		path string
		want string
		found bool
	}{
		{"top level", "status", "ok", true},
		{"nested bool", "data.synced", "true", true},
		{"nested number", "data.accountsOnline", "1200", true},
		{"missing", "data.unknown", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, found := extractJSONField(data, tc.path)
			if got != tc.want || found != tc.found {
				t.Fatalf("got (%q, %v), want (%q, %v)", got, found, tc.want, tc.found)
			}
		})
	}
}

func TestEvaluateRuleBoundaryValues(t *testing.T) {
	cases := []struct {
		name string
		current string
		op monitor.Operator
		threshold string
		violated bool
	}{
		{"gte equal passes", "100", monitor.OpGte, "100", false},
		{"gte below violates", "99", monitor.OpGte, "100", true},
		{"lte equal passes", "100", monitor.OpLte, "100", false},
		{"lte above violates", "101", monitor.OpLte, "100", true},
		{"eq same passes", "true", monitor.OpEq, "true", false},
		{"eq different violates", "false", monitor.OpEq, "true", true},
		{"contains passes", "payment completed", monitor.OpContains, "completed", false},
		{"contains violates", "payment failed", monitor.OpContains, "completed", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violated, _ := evaluateRule(tc.current, tc.op, tc.threshold)
			if violated != tc.violated {
				t.Fatalf("violated=%v, want %v", violated, tc.violated)
			}
		})
	}
}
