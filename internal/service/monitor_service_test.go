package service

import (
	"testing"

	"github.com/JanGustavo/Cron/internal/domain/monitor"
)

func TestEvaluateRule(t *testing.T) {
	tests := []struct {
		name         string
		currentVal   string
		op           monitor.Operator
		thresholdVal string
		wantViolated bool
	}{
		// Operador gt (esperado > threshold, violado se <=)
		{"gt passed", "1050", monitor.OpGt, "1000", false},
		{"gt violated equal", "1000", monitor.OpGt, "1000", true},
		{"gt violated less", "450", monitor.OpGt, "1000", true},

		// Operador gte (esperado >= threshold, violado se <)
		{"gte passed equal", "1000", monitor.OpGte, "1000", false},
		{"gte passed greater", "1200", monitor.OpGte, "1000", false},
		{"gte violated", "999", monitor.OpGte, "1000", true},

		// Operador lt (esperado < threshold, violado se >=)
		{"lt passed", "450", monitor.OpLt, "1000", false},
		{"lt violated equal", "1000", monitor.OpLt, "1000", true},
		{"lt violated greater", "1500", monitor.OpLt, "1000", true},

		// Operador lte (esperado <= threshold, violado se >)
		{"lte passed equal", "1000", monitor.OpLte, "1000", false},
		{"lte passed less", "800", monitor.OpLte, "1000", false},
		{"lte violated", "1001", monitor.OpLte, "1000", true},

		// Operador eq (esperado == threshold, violado se !=)
		{"eq passed", "active", monitor.OpEq, "active", false},
		{"eq violated", "inactive", monitor.OpEq, "active", true},

		// Operador ne (esperado != threshold, violado se ==)
		{"ne passed", "healthy", monitor.OpNe, "unhealthy", false},
		{"ne violated", "unhealthy", monitor.OpNe, "unhealthy", true},

		// Operador contains (esperado conter substring)
		{"contains passed", "error: database connection lost", monitor.OpContains, "database", false},
		{"contains violated", "all systems operational", monitor.OpContains, "error", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violated, msg := evaluateRule(tt.currentVal, tt.op, tt.thresholdVal)
			if violated != tt.wantViolated {
				t.Errorf("evaluateRule(%s, %s, %s) = violated:%v (msg: %s), wantViolated:%v",
					tt.currentVal, tt.op, tt.thresholdVal, violated, msg, tt.wantViolated)
			}
		})
	}
}

func TestExtractJSONField(t *testing.T) {
	data := map[string]any{
		"goroutines_count": float64(8),
		"status":           "healthy",
		"metrics": map[string]any{
			"accountsOnline": float64(1250),
			"active":         true,
			"details": map[string]any{
				"version": "1.2.0",
			},
		},
	}

	tests := []struct {
		path      string
		wantVal   string
		wantFound bool
	}{
		{"goroutines_count", "8", true},
		{"status", "healthy", true},
		{"metrics.accountsOnline", "1250", true},
		{"metrics.active", "true", true},
		{"metrics.details.version", "1.2.0", true},
		{"non_existent", "", false},
		{"metrics.non_existent", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			val, found := extractJSONField(data, tt.path)
			if found != tt.wantFound || val != tt.wantVal {
				t.Errorf("extractJSONField(data, %q) = (%q, %v), want (%q, %v)",
					tt.path, val, found, tt.wantVal, tt.wantFound)
			}
		})
	}
}

