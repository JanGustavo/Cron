package postgres

// ExecutionRepository — acesso ao banco para Execution.
// Métodos:
//   - Create(ctx, execution)
//   - ListByJob(ctx, jobID, limit)
//   - CountConsecutiveFailures(ctx, jobID)
//   - DeleteOlderThan(ctx, days)    ← job de retenção (7d/90d)

type ExecutionRepository struct{}
