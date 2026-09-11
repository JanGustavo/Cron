package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/JanGustavo/Cron/internal/domain/monitor"
)

type MonitorRepository struct {
	db *sql.DB
}

func NewMonitorRepository(db *sql.DB) *MonitorRepository {
	return &MonitorRepository{db: db}
}

// UpsertKeyValue salva ou atualiza o último valor conhecido para uma chave de um projeto.
func (r *MonitorRepository) UpsertKeyValue(ctx context.Context, projectID, key, value string) (*monitor.KeyValue, error) {
	query := `
		INSERT INTO monitor_key_values (project_id, key, current_value, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (project_id, key)
		DO UPDATE SET current_value = EXCLUDED.current_value, updated_at = NOW()
		RETURNING project_id, key, current_value, updated_at;
	`
	kv := &monitor.KeyValue{}
	err := r.db.QueryRowContext(ctx, query, projectID, key, value).Scan(
		&kv.ProjectID,
		&kv.Key,
		&kv.CurrentValue,
		&kv.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("MonitorRepository.UpsertKeyValue: %w", err)
	}
	return kv, nil
}

// GetKeyValue busca o último valor registrado de uma chave para um projeto.
func (r *MonitorRepository) GetKeyValue(ctx context.Context, projectID, key string) (*monitor.KeyValue, error) {
	query := `
		SELECT project_id, key, current_value, updated_at
		FROM monitor_key_values
		WHERE project_id = $1 AND key = $2;
	`
	kv := &monitor.KeyValue{}
	err := r.db.QueryRowContext(ctx, query, projectID, key).Scan(
		&kv.ProjectID,
		&kv.Key,
		&kv.CurrentValue,
		&kv.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("MonitorRepository.GetKeyValue: %w", err)
	}
	return kv, nil
}

// CreateRule insere uma nova regra de monitoramento.
func (r *MonitorRepository) CreateRule(ctx context.Context, rule *monitor.MonitorRule) (*monitor.MonitorRule, error) {
	query := `
		INSERT INTO monitor_rules (project_id, job_id, name, key, operator, threshold_value, alert_email, webhook_url, is_enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, project_id, job_id, name, key, operator, threshold_value, alert_email, webhook_url, is_enabled, created_at, updated_at;
	`
	created := &monitor.MonitorRule{}
	err := r.db.QueryRowContext(
		ctx, query,
		rule.ProjectID, rule.JobID, rule.Name, rule.Key, string(rule.Operator), rule.ThresholdValue, rule.AlertEmail, rule.WebhookURL, rule.IsEnabled,
	).Scan(
		&created.ID,
		&created.ProjectID,
		&created.JobID,
		&created.Name,
		&created.Key,
		&created.Operator,
		&created.ThresholdValue,
		&created.AlertEmail,
		&created.WebhookURL,
		&created.IsEnabled,
		&created.CreatedAt,
		&created.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("MonitorRepository.CreateRule: %w", err)
	}
	return created, nil
}

// ListRulesByKey lista todas as regras ativas de um projeto associadas a uma determinada chave.
func (r *MonitorRepository) ListRulesByKey(ctx context.Context, projectID, key string) ([]*monitor.MonitorRule, error) {
	query := `
		SELECT id, project_id, job_id, name, key, operator, threshold_value, alert_email, webhook_url, is_enabled, created_at, updated_at
		FROM monitor_rules
		WHERE project_id = $1 AND key = $2 AND is_enabled = true;
	`
	rows, err := r.db.QueryContext(ctx, query, projectID, key)
	if err != nil {
		return nil, fmt.Errorf("MonitorRepository.ListRulesByKey: %w", err)
	}
	defer rows.Close()

	var rules []*monitor.MonitorRule
	for rows.Next() {
		rule := &monitor.MonitorRule{}
		if err := rows.Scan(
			&rule.ID,
			&rule.ProjectID,
			&rule.JobID,
			&rule.Name,
			&rule.Key,
			&rule.Operator,
			&rule.ThresholdValue,
			&rule.AlertEmail,
			&rule.WebhookURL,
			&rule.IsEnabled,
			&rule.CreatedAt,
			&rule.UpdatedAt,
		); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// ListRulesByJob lista todas as regras ativas vinculadas a um job específico OU globais (job_id IS NULL).
func (r *MonitorRepository) ListRulesByJob(ctx context.Context, projectID, jobID string) ([]*monitor.MonitorRule, error) {
	query := `
		SELECT id, project_id, job_id, name, key, operator, threshold_value, alert_email, webhook_url, is_enabled, created_at, updated_at
		FROM monitor_rules
		WHERE project_id = $1 AND (job_id = $2 OR job_id IS NULL) AND is_enabled = true;
	`
	rows, err := r.db.QueryContext(ctx, query, projectID, jobID)
	if err != nil {
		return nil, fmt.Errorf("MonitorRepository.ListRulesByJob: %w", err)
	}
	defer rows.Close()

	var rules []*monitor.MonitorRule
	for rows.Next() {
		rule := &monitor.MonitorRule{}
		if err := rows.Scan(
			&rule.ID,
			&rule.ProjectID,
			&rule.JobID,
			&rule.Name,
			&rule.Key,
			&rule.Operator,
			&rule.ThresholdValue,
			&rule.AlertEmail,
			&rule.WebhookURL,
			&rule.IsEnabled,
			&rule.CreatedAt,
			&rule.UpdatedAt,
		); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// ListRulesByProject lista todas as regras de um projeto.
func (r *MonitorRepository) ListRulesByProject(ctx context.Context, projectID string) ([]*monitor.MonitorRule, error) {
	query := `
		SELECT id, project_id, job_id, name, key, operator, threshold_value, alert_email, webhook_url, is_enabled, created_at, updated_at
		FROM monitor_rules
		WHERE project_id = $1
		ORDER BY created_at DESC;
	`
	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, fmt.Errorf("MonitorRepository.ListRulesByProject: %w", err)
	}
	defer rows.Close()

	var rules []*monitor.MonitorRule
	for rows.Next() {
		rule := &monitor.MonitorRule{}
		if err := rows.Scan(
			&rule.ID,
			&rule.ProjectID,
			&rule.JobID,
			&rule.Name,
			&rule.Key,
			&rule.Operator,
			&rule.ThresholdValue,
			&rule.AlertEmail,
			&rule.WebhookURL,
			&rule.IsEnabled,
			&rule.CreatedAt,
			&rule.UpdatedAt,
		); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// UpdateRule atualiza uma regra de monitoramento existente.
func (r *MonitorRepository) UpdateRule(ctx context.Context, rule *monitor.MonitorRule) (*monitor.MonitorRule, error) {
	query := `
		UPDATE monitor_rules SET
			name = $1,
			key = $2,
			operator = $3,
			threshold_value = $4,
			alert_email = $5,
			webhook_url = $6,
			is_enabled = $7,
			job_id = $8,
			updated_at = NOW()
		WHERE id = $9 AND project_id = $10
		RETURNING id, project_id, job_id, name, key, operator, threshold_value, alert_email, webhook_url, is_enabled, created_at, updated_at;
	`

	updated := &monitor.MonitorRule{}
	err := r.db.QueryRowContext(ctx, query,
		rule.Name,
		rule.Key,
		string(rule.Operator),
		rule.ThresholdValue,
		rule.AlertEmail,
		rule.WebhookURL,
		rule.IsEnabled,
		rule.JobID,
		rule.ID,
		rule.ProjectID,
	).Scan(
		&updated.ID,
		&updated.ProjectID,
		&updated.JobID,
		&updated.Name,
		&updated.Key,
		&updated.Operator,
		&updated.ThresholdValue,
		&updated.AlertEmail,
		&updated.WebhookURL,
		&updated.IsEnabled,
		&updated.CreatedAt,
		&updated.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("MonitorRepository.UpdateRule: %w", err)
	}
	return updated, nil
}

// DeleteRule remove uma regra pelo ID garantindo pertencimento ao projeto.
func (r *MonitorRepository) DeleteRule(ctx context.Context, id, projectID string) error {
	query := `DELETE FROM monitor_rules WHERE id = $1 AND project_id = $2;`
	res, err := r.db.ExecContext(ctx, query, id, projectID)
	if err != nil {
		return fmt.Errorf("MonitorRepository.DeleteRule: %w", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
