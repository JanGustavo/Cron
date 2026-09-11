package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/JanGustavo/Cron/internal/domain/execution"
	"github.com/JanGustavo/Cron/internal/domain/monitor"
)

// CreateWithRuleEvaluations cria a execução e registra o resultado das regras no mesmo INSERT.
// O JSON é mantido como JSONB para que o histórico continue consultável no PostgreSQL.
func (r *ExecutionRepository) CreateWithRuleEvaluations(ctx context.Context, e *execution.Execution, evaluations []monitor.RuleStatus) error {
	var raw []byte
	var err error
	if evaluations != nil {
		raw, err = json.Marshal(evaluations)
		if err != nil {
			return fmt.Errorf("ExecutionRepository.CreateWithRuleEvaluations marshal: %w", err)
		}
	}

	var ruleEvaluations interface{}
	if len(raw) > 0 {
		ruleEvaluations = raw
	}

	query := `
		INSERT INTO executions
			(job_id, status, http_status, duration_ms, response_body, attempt_number, rule_evaluations)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`

	if err := r.db.QueryRowContext(ctx, query,
		e.JobID, e.Status, e.HTTPStatus, e.DurationMs, e.ResponseBody, e.AttemptNumber, ruleEvaluations,
	).Scan(&e.ID); err != nil {
		return fmt.Errorf("ExecutionRepository.CreateWithRuleEvaluations: %w", err)
	}

	return nil
}

// GetRuleEvaluations decodifica o histórico de avaliações de uma execução.
func (r *ExecutionRepository) GetRuleEvaluations(ctx context.Context, executionID string) ([]monitor.RuleStatus, error) {
	var raw []byte
	err := r.db.QueryRowContext(ctx,
		`SELECT rule_evaluations FROM executions WHERE id = $1`, executionID,
	).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ExecutionRepository.GetRuleEvaluations: %w", err)
	}
	if len(raw) == 0 {
		return []monitor.RuleStatus{}, nil
	}

	var evaluations []monitor.RuleStatus
	if err := json.Unmarshal(raw, &evaluations); err != nil {
		return nil, fmt.Errorf("ExecutionRepository.GetRuleEvaluations decode: %w", err)
	}
	return evaluations, nil
}
