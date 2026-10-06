package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/JanGustavo/Cron/internal/domain/monitor"
)

// Unimplemented repository methods panic, detecting writes from preview/evaluation.
type previewRepository struct {
	monitorRepository
	rules   []*monitor.MonitorRule
	belongs bool
}

func (r *previewRepository) ListRulesByKey(context.Context, string, string) ([]*monitor.MonitorRule, error) {
	return r.rules, nil
}
func (r *previewRepository) ListRulesByJob(context.Context, string, string) ([]*monitor.MonitorRule, error) {
	return r.rules, nil
}
func (r *previewRepository) JobBelongsToProject(context.Context, string, string) (bool, error) {
	return r.belongs, nil
}

type previewAlerts struct{ calls int }

func (a *previewAlerts) NotifyKeyViolation(string, string, string, string, string, string, string, string, string) {
	a.calls++
}
func (a *previewAlerts) NotifyEmailKeyViolation(string, string, string, string, string, string) {
	a.calls++
}

func TestPreviewIsReadOnlyAndScoped(t *testing.T) {
	repo := &previewRepository{belongs: true, rules: []*monitor.MonitorRule{
		{ID: "count-rule", Key: "count", Operator: monitor.OpGt, ThresholdValue: "10", AlertEmail: true},
		{ID: "other-rule", Key: "status", Operator: monitor.OpEq, ThresholdValue: "ok", AlertEmail: true},
	}}
	alerts := &previewAlerts{}
	service := NewMonitorService(repo, alerts, "")
	result, err := service.PreviewKey(context.Background(), "project", "job", "count", json.Number("0"))
	if err != nil || len(result.Rules) != 1 || !result.Evaluated || !result.Violated {
		t.Fatalf("unexpected preview %+v, %v", result, err)
	}
	result, err = service.PreviewKey(context.Background(), "project", "job", "unknown", false)
	if err != nil || result.Evaluated || result.Violated {
		t.Fatalf("unmatched key claimed success %+v, %v", result, err)
	}
	if alerts.calls != 0 {
		t.Fatal("preview dispatched real notifications")
	}
	repo.belongs = false
	if _, err = service.PreviewKey(context.Background(), "project", "foreign-job", "count", json.Number("0")); err == nil {
		t.Fatal("preview accepted foreign job")
	}
}

func TestJobEvaluationDoesNotOverwriteGlobalKeyValues(t *testing.T) {
	repo := &previewRepository{rules: []*monitor.MonitorRule{{ID: "rule", Key: "count", Operator: monitor.OpGt, ThresholdValue: "0"}}}
	service := NewMonitorService(repo, nil, "")
	violated, statuses, err := service.EvaluateJobPayload(context.Background(), "project", "job", []byte(`{"count":0}`))
	if err != nil || !violated || len(statuses) != 1 {
		t.Fatalf("unexpected job evaluation: %+v %v", statuses, err)
	}
}

func TestMissingFieldTriggersViolationNotification(t *testing.T) {
	alerts := &previewAlerts{}
	webhook := "https://example.com/alerts"
	repo := &previewRepository{rules: []*monitor.MonitorRule{{ID: "rule", Key: "count", Operator: monitor.OpGt, ThresholdValue: "0", AlertEmail: true, WebhookURL: &webhook}}}
	service := NewMonitorService(repo, alerts, "")
	violated, statuses, err := service.EvaluateJobPayload(context.Background(), "project", "job", []byte(`{}`))
	if err != nil || !violated || len(statuses) != 1 || statuses[0].Reason != "missing_field" || alerts.calls != 2 {
		t.Fatalf("missing field silently ignored: %+v, alerts=%d, err=%v", statuses, alerts.calls, err)
	}
}

func TestCandidatePreviewIsTypedScopedAndHasNoSideEffects(t *testing.T) {
	repo := &previewRepository{belongs: true}
	alerts := &previewAlerts{}
	service := NewMonitorService(repo, alerts, "")
	jobID := "job"
	for _, tc := range []struct {
		typ, op, expected, body string
		valid                   bool
	}{
		{"boolean", "eq", "true", `{"value":false}`, true},
		{"boolean", "eq", "1", `{"value":true}`, false},
		{"string", "eq", "", `{"value":""}`, true},
		{"string", "gt", "1", `{"value":"2"}`, false},
		{"number", "eq", "9007199254740993", `{"value":9007199254740993}`, true},
		{"null", "eq", "null", `{"value":null}`, true},
	} {
		rule := &monitor.MonitorRule{ProjectID: "project", JobID: &jobID, Key: "/value", Operator: monitor.Operator(tc.op), ValueType: tc.typ, ThresholdValue: tc.expected, Mode: monitor.ModeRequireSuccess, AlertEmail: true}
		result, err := service.PreviewRule(context.Background(), rule, []byte(tc.body))
		if (err == nil) != tc.valid {
			t.Fatalf("candidate %s/%s expected valid=%v: %v", tc.typ, tc.op, tc.valid, err)
		}
		if tc.valid && (len(result) != 1 || result[0].Mode != monitor.ModeRequireSuccess) {
			t.Fatalf("missing intention: %+v", result)
		}
	}
	if alerts.calls != 0 {
		t.Fatal("candidate preview notified")
	}
	global := &monitor.MonitorRule{Name: "Required", Key: "/value", Mode: monitor.ModeRequireSuccess, Operator: monitor.OpEq, ThresholdValue: "true"}
	if _, err := service.PreviewRule(context.Background(), global, []byte(`{}`)); err == nil {
		t.Fatal("global success condition accepted")
	}
	repo.belongs = false
	global.JobID = &jobID
	if _, err := service.PreviewRule(context.Background(), global, []byte(`{}`)); err == nil {
		t.Fatal("foreign job accepted")
	}
}
