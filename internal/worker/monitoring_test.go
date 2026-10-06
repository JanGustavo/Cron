package worker

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/JanGustavo/Cron/pkg/httputil"
	"testing"
	"time"

	"github.com/JanGustavo/Cron/internal/domain/billing"
	"github.com/JanGustavo/Cron/internal/domain/execution"
	"github.com/JanGustavo/Cron/internal/domain/job"
	"github.com/JanGustavo/Cron/internal/domain/monitor"
	"github.com/JanGustavo/Cron/internal/queue"
	"github.com/hibiken/asynq"
)

type testJobs struct {
	value              *job.Job
	limits             *billing.PlanLimits
	resets, increments int
}

func (r *testJobs) FindByID(context.Context, string) (*job.Job, error) { return r.value, nil }
func (r *testJobs) ResetFailures(context.Context, string) error        { r.resets++; return nil }
func (r *testJobs) IncrementFailures(context.Context, string) error    { r.increments++; return nil }

type testExecutions struct {
	value       *execution.Execution
	evaluations []monitor.RuleStatus
}

func (r *testExecutions) CreateWithRuleEvaluations(_ context.Context, e *execution.Execution, s []monitor.RuleStatus) error {
	r.value = e
	r.evaluations = s
	return nil
}

type testQueue struct{ calls int }

func (q *testQueue) Enqueue(context.Context, string) error { q.calls++; return nil }

type testMonitor struct {
	mode        string
	unavailable bool
}

func (m testMonitor) EvaluateJobResponse(_ context.Context, _, _ string, response monitor.Response) (bool, []monitor.RuleStatus, error) {
	if m.unavailable {
		return false, nil, errors.New("database unavailable")
	}
	results := monitor.EvaluateResponse([]*monitor.MonitorRule{{Mode: m.mode, ValueType: "boolean", Key: "/synced", Operator: monitor.OpEq, ThresholdValue: "true"}}, response)
	return monitor.HasViolation(results), results, nil
}
func TestRequiredResponseControlsPipelineWithoutRetryOrSuspension(t *testing.T) {
	cases := []struct {
		name, body, mode string
		unavailable      bool
		status           execution.Status
		chain            int
	}{
		{"required false", `{"synced":false}`, monitor.ModeRequireSuccess, false, execution.StatusValidationFailed, 0},
		{"required missing", `{}`, monitor.ModeRequireSuccess, false, execution.StatusValidationFailed, 0},
		{"required invalid JSON", `broken`, monitor.ModeRequireSuccess, false, execution.StatusValidationFailed, 0},
		{"required true", `{"synced":true}`, monitor.ModeRequireSuccess, false, execution.StatusSuccess, 1},
		{"observe false", `{"synced":false}`, monitor.ModeObserve, false, execution.StatusSuccess, 1},
		{"loading failure", `{"synced":true}`, monitor.ModeRequireSuccess, true, execution.StatusValidationFailed, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {

			next := "next"
			jobs := &testJobs{value: &job.Job{ID: "job", ProjectID: "project", Status: job.StatusActive, URL: "https://example.com/job", HTTPMethod: job.MethodGet, NextJobID: &next}}
			history := &testExecutions{}
			queued := &testQueue{}
			worker := &Worker{jobRepo: jobs, executionRepo: history, enqueuer: queued, monitorService: testMonitor{tc.mode, tc.unavailable}, executeHTTP: func(context.Context, string, string, map[string]string, map[string]any, time.Duration) (*httputil.Result, error) {
				return &httputil.Result{StatusCode: 200, Body: tc.body}, nil
			}}
			payload, _ := json.Marshal(queue.HTTPJobPayload{JobID: "job"})
			if err := worker.ProcessTask(context.Background(), asynq.NewTask("http", payload)); err != nil {
				t.Fatalf("validation scheduled retry: %v", err)
			}
			if history.value == nil || history.value.Status != tc.status || history.value.TransportStatus != execution.StatusSuccess {
				t.Fatalf("wrong outcome: %+v", history.value)
			}
			if queued.calls != tc.chain || jobs.increments != 0 || jobs.resets != 1 {
				t.Fatalf("wrong pipeline/retry policy: chain=%d failures=%d resets=%d", queued.calls, jobs.increments, jobs.resets)
			}
		})
	}
}

func (r *testJobs) ExecutionLimits(context.Context, string) (*billing.PlanLimits, error) {
	if r.limits != nil {
		return r.limits, nil
	}
	return &billing.PlanLimits{WorkflowsEnabled: true, AlertsWebhooksEnabled: true}, nil
}

func TestFreeGraceExecutesHTTPWithoutPaidWorkflow(t *testing.T) {
	next := "next"
	jobs := &testJobs{value: &job.Job{ID: "job", ProjectID: "project", Status: job.StatusActive, URL: "https://example.invalid", HTTPMethod: job.MethodGet, NextJobID: &next}, limits: &billing.PlanLimits{Code: "free"}}
	queued := &testQueue{}
	history := &testExecutions{}
	calls := 0
	w := &Worker{jobRepo: jobs, enqueuer: queued, executionRepo: history, executeHTTP: func(context.Context, string, string, map[string]string, map[string]any, time.Duration) (*httputil.Result, error) {
		calls++
		return &httputil.Result{StatusCode: 200, Body: "{}"}, nil
	}}
	payload, _ := json.Marshal(queue.HTTPJobPayload{JobID: "job"})
	if err := w.ProcessTask(context.Background(), asynq.NewTask("http", payload)); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || queued.calls != 0 {
		t.Fatalf("Free grace HTTP=%d paid chain=%d", calls, queued.calls)
	}
	jobs.value.Status = job.StatusPaused
	if err := w.ProcessTask(context.Background(), asynq.NewTask("http", payload)); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("paused queued job executed")
	}
}
