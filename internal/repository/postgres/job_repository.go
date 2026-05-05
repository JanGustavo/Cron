package postgres

// JobRepository implementa o acesso ao banco para a entidade Job.
// Gerado parcialmente pelo sqlc a partir de /migrations/queries/jobs.sql.
// Métodos:
//   - Create(ctx, job)
//   - FindByID(ctx, id)
//   - ListByProject(ctx, projectID)
//   - FindEligibleToRun(ctx, now)    ← query crítica do Scheduler
//   - UpdateNextRun(ctx, id, nextRun)
//   - UpdateStatus(ctx, id, status)

type JobRepository struct{}
