package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

type maintenanceProbe struct {
	calls   int
	failure error
}

func (p *maintenanceProbe) DeleteExpiredExecutions(context.Context) (int64, error) {
	p.calls++
	return 0, p.failure
}
func (*maintenanceProbe) CleanupExpiredUnverifiedUsers(context.Context) error {
	panic("log retention must not trigger unrelated account deletion")
}

func TestRetentionRunsAtStartupAndByElapsedTime(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	for _, interval := range []time.Duration{time.Second, 30 * time.Second, 5 * time.Minute} {
		probe := &maintenanceProbe{}
		scheduler := &Scheduler{executionRepo: probe, interval: interval}
		scheduler.cleanupExecutions(ctx, now)
		scheduler.cleanupExecutions(ctx, now.Add(59*time.Minute))
		if probe.calls != 1 {
			t.Fatalf("cleanup did not run once at startup: %d", probe.calls)
		}
		scheduler.cleanupExecutions(ctx, now.Add(time.Hour))
		if probe.calls != 2 {
			t.Fatal("cleanup depends on tick count instead of elapsed time")
		}
		restarted := &Scheduler{executionRepo: probe, interval: interval}
		restarted.cleanupExecutions(ctx, now.Add(time.Hour+time.Minute))
		if probe.calls != 3 {
			t.Fatal("restart delayed cleanup")
		}
	}
}
func TestRetentionRetriesFailedCleanup(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	probe := &maintenanceProbe{failure: errors.New("unavailable")}
	scheduler := &Scheduler{executionRepo: probe}
	scheduler.cleanupExecutions(ctx, now)
	scheduler.cleanupExecutions(ctx, now.Add(30*time.Second))
	if probe.calls != 1 {
		t.Fatal("failure retried every tick")
	}
	probe.failure = nil
	scheduler.cleanupExecutions(ctx, now.Add(time.Minute))
	if probe.calls != 2 {
		t.Fatal("failed cleanup postponed for a full day")
	}
	scheduler.cleanupExecutions(ctx, now.Add(2*time.Minute))
	if probe.calls != 2 {
		t.Fatal("successful cleanup was not rescheduled")
	}
}
