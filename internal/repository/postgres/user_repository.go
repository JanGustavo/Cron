package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/JanGustavo/Cron/internal/domain/project"
	"github.com/JanGustavo/Cron/internal/domain/user"
)

// UserRepository — acesso ao banco para User e APIKey.
// NUNCA armazena ou retorna a API Key em plain text.
// Métodos:
//   - Create(ctx, user)
//   - FindByEmail(ctx, email)
//   - FindProjectByKeyHash(ctx, hash)  ← autenticação

type UserRepository struct{ 
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository{
	return &UserRepository{db: db}
}

// GetDB retorna a conexão do banco para queries diretas (uso interno/admin)
func (r *UserRepository) GetDB() *sql.DB {
	return r.db
}

// CreateUser insere um novo usuário no banco.
func (r *UserRepository) CreateUser(ctx context.Context, email string) (*user.User, error) {
	u := &user.User{}
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO users (email) VALUES ($1) RETURNING id, email, plan, COALESCE(password_hash, ''), email_alerts_enabled, daily_digest_enabled, COALESCE(timezone, 'America/Sao_Paulo'), COALESCE(digest_hour, 18), last_digest_sent_at, is_verified, created_at`,
		email,
	).Scan(&u.ID, &u.Email, &u.Plan, &u.PasswordHash, &u.EmailAlertsEnabled, &u.DailyDigestEnabled, &u.Timezone, &u.DigestHour, &u.LastDigestSentAt, &u.IsVerified, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("UserRepository.CreateUser: %w", err)
	}
	return u, nil
}

// CreateUserWithPassword insere um novo usuário com senha, nome completo e CPF no banco.
func (r *UserRepository) CreateUserWithPassword(ctx context.Context, email, passwordHash, fullName, cpf string) (*user.User, error) {
	u := &user.User{}
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO users (email, password_hash, full_name, cpf) VALUES ($1, $2, $3, NULLIF($4, '')) RETURNING id, email, plan, COALESCE(password_hash, ''), COALESCE(full_name, ''), COALESCE(cpf, ''), email_alerts_enabled, daily_digest_enabled, COALESCE(timezone, 'America/Sao_Paulo'), COALESCE(digest_hour, 18), last_digest_sent_at, is_verified, created_at`,
		email, passwordHash, fullName, cpf,
	).Scan(&u.ID, &u.Email, &u.Plan, &u.PasswordHash, &u.FullName, &u.CPF, &u.EmailAlertsEnabled, &u.DailyDigestEnabled, &u.Timezone, &u.DigestHour, &u.LastDigestSentAt, &u.IsVerified, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("UserRepository.CreateUserWithPassword: %w", err)
	}
	return u, nil
}

// FindByEmail busca um usuário pelo email.
func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*user.User, error) {
	u := &user.User{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, email, plan, COALESCE(password_hash, ''), COALESCE(full_name, ''), COALESCE(cpf, ''), email_alerts_enabled, daily_digest_enabled, COALESCE(timezone, 'America/Sao_Paulo'), COALESCE(digest_hour, 18), last_digest_sent_at, is_verified, created_at FROM users WHERE email = $1 LIMIT 1`,
		email,
	).Scan(&u.ID, &u.Email, &u.Plan, &u.PasswordHash, &u.FullName, &u.CPF, &u.EmailAlertsEnabled, &u.DailyDigestEnabled, &u.Timezone, &u.DigestHour, &u.LastDigestSentAt, &u.IsVerified, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("UserRepository.FindByEmail: %w", err)
	}
	return u, nil
}

// FindByCPF busca um usuário pelo CPF.
func (r *UserRepository) FindByCPF(ctx context.Context, cpf string) (*user.User, error) {
	u := &user.User{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, email, plan, COALESCE(password_hash, ''), COALESCE(full_name, ''), COALESCE(cpf, ''), email_alerts_enabled, daily_digest_enabled, COALESCE(timezone, 'America/Sao_Paulo'), COALESCE(digest_hour, 18), last_digest_sent_at, is_verified, created_at FROM users WHERE cpf = $1 LIMIT 1`,
		cpf,
	).Scan(&u.ID, &u.Email, &u.Plan, &u.PasswordHash, &u.FullName, &u.CPF, &u.EmailAlertsEnabled, &u.DailyDigestEnabled, &u.Timezone, &u.DigestHour, &u.LastDigestSentAt, &u.IsVerified, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("UserRepository.FindByCPF: %w", err)
	}
	return u, nil
}

// FindProjectsByUserID retorna todos os projetos de um usuário.
func (r *UserRepository) FindProjectsByUserID(ctx context.Context, userID string) ([]*project.Project, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, name, created_at, webhook_secret FROM projects WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("UserRepository.FindProjectsByUserID: %w", err)
	}
	defer rows.Close()

	var projects []*project.Project
	for rows.Next() {
		p := &project.Project{}
		if err := rows.Scan(&p.ID, &p.UserID, &p.Name, &p.CreatedAt, &p.WebhookSecret); err != nil {
			return nil, fmt.Errorf("UserRepository.FindProjectsByUserID scan: %w", err)
		}
		projects = append(projects, p)
	}
	return projects, nil
}

// CreateProject insere um projeto vinculado a um usuário.
func (r *UserRepository) CreateProject(ctx context.Context, userID, name string) (*project.Project, error) {
	p := &project.Project{}
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO projects (user_id, name) VALUES ($1, $2) RETURNING id, user_id, name, created_at, webhook_secret`,
		userID, name,
	).Scan(&p.ID, &p.UserID, &p.Name, &p.CreatedAt, &p.WebhookSecret)
	if err != nil {
		return nil, fmt.Errorf("UserRepository.CreateProject: %w", err)
	}
	return p, nil
}

// UpdateProjectWebhookSecret atualiza o segredo de webhook de um projeto específico.
func (r *UserRepository) UpdateProjectWebhookSecret(ctx context.Context, projectID, secret string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE projects SET webhook_secret = $1 WHERE id = $2`,
		secret, projectID,
	)
	if err != nil {
		return fmt.Errorf("UserRepository.UpdateProjectWebhookSecret: %w", err)
	}
	return nil
}

// CreateAPIKey salva o hash da key no banco — nunca o plain text.
// prefix é só o início da key (ex: "cf_live_abc1") para identificar em logs.
func (r *UserRepository) CreateAPIKey(ctx context.Context, projectID, keyHash, prefix string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO api_keys (project_id, key_hash, prefix) VALUES ($1, $2, $3)`,
		projectID, keyHash, prefix,
	)
	if err != nil {
		return fmt.Errorf("UserRepository.CreateAPIKey: %w", err)
	}
	return nil
}

// FindProjectByKeyHash é o método central da autenticação.
// Recebe o SHA-256 da API Key e retorna o Project associado.
// Retorna nil, nil se a key não existir — o middleware decide o que fazer.
//
// O JOIN garante que uma key revogada (api_key deletada) não autentica,
// mesmo que o project_id ainda exista.
func (r *UserRepository) FindProjectByKeyHash(ctx context.Context, keyHash string) (*project.Project, error) {
	p := &project.Project{}
	err := r.db.QueryRowContext(ctx, `
		SELECT p.id, p.user_id, p.name, p.created_at
		FROM projects p
		INNER JOIN api_keys k ON k.project_id = p.id
		WHERE k.key_hash = $1
		LIMIT 1`,
		keyHash,
	).Scan(&p.ID, &p.UserID, &p.Name, &p.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("UserRepository.FindProjectByKeyHash: %w", err)
	}

	// Atualiza o last_used_at de forma assíncrona para não onerar o tempo de resposta da API
	go func(hash string) {
		_, _ = r.db.Exec(`UPDATE api_keys SET last_used_at = NOW() WHERE key_hash = $1`, hash)
	}(keyHash)

	return p, nil
}

// FindUserByProjectID retorna o usuário dono de um projeto.
// Usado pelo JobService para verificar o plano e aplicar limites.
func (r *UserRepository) FindUserByProjectID(ctx context.Context, projectID string) (*user.User, error) {
	u := &user.User{}
	err := r.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.plan, COALESCE(u.password_hash, ''), COALESCE(u.full_name, ''), COALESCE(u.cpf, ''), u.email_alerts_enabled, u.daily_digest_enabled, COALESCE(u.timezone, 'America/Sao_Paulo'), COALESCE(u.digest_hour, 18), u.last_digest_sent_at, u.is_verified, COALESCE(u.role, 'user'), COALESCE(u.ai_queries_used, 0), u.created_at
		FROM users u
		INNER JOIN projects p ON p.user_id = u.id
		WHERE p.id = $1`,
		projectID,
	).Scan(&u.ID, &u.Email, &u.Plan, &u.PasswordHash, &u.FullName, &u.CPF, &u.EmailAlertsEnabled, &u.DailyDigestEnabled, &u.Timezone, &u.DigestHour, &u.LastDigestSentAt, &u.IsVerified, &u.Role, &u.AiQueriesUsed, &u.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("UserRepository.FindUserByProjectID: %w", err)
	}
	return u, nil
}

type APIKey struct {
	ID         string     `json:"id"`
	ProjectID  string     `json:"projectId"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

// FindAPIKeysByProjectID retorna todas as API Keys ativas vinculadas a um projeto.
// O hash nunca é retornado por motivos de segurança.
func (r *UserRepository) FindAPIKeysByProjectID(ctx context.Context, projectID string) ([]*APIKey, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, project_id, prefix, created_at, last_used_at FROM api_keys WHERE project_id = $1 ORDER BY created_at DESC`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("UserRepository.FindAPIKeysByProjectID: %w", err)
	}
	defer rows.Close()

	var keys []*APIKey
	for rows.Next() {
		k := &APIKey{}
		if err := rows.Scan(&k.ID, &k.ProjectID, &k.Prefix, &k.CreatedAt, &k.LastUsedAt); err != nil {
			return nil, fmt.Errorf("UserRepository.FindAPIKeysByProjectID scan: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// DeleteAPIKey remove/revoga uma chave de API específica por ID e project_id (para segurança extra).
func (r *UserRepository) DeleteAPIKey(ctx context.Context, id, projectID string) error {
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM api_keys WHERE id = $1 AND project_id = $2`,
		id,
		projectID,
	)
	if err != nil {
		return fmt.Errorf("UserRepository.DeleteAPIKey: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("API Key não encontrada ou você não tem permissão para revogá-la")
	}
	return nil
}

// UpdatePassword altera a senha de um usuário no banco.
func (r *UserRepository) UpdatePassword(ctx context.Context, userID, newPasswordHash string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET password_hash = $1 WHERE id = $2`,
		newPasswordHash, userID,
	)
	if err != nil {
		return fmt.Errorf("UserRepository.UpdatePassword: %w", err)
	}
	return nil
}

// UpdateUserProfile atualiza dados cadastrais e preferências do usuário.
func (r *UserRepository) UpdateUserProfile(ctx context.Context, userID, fullName, cpf, timezone string, emailAlerts, dailyDigest bool, digestHour int) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users 
		 SET full_name = $1, cpf = $2, timezone = $3, email_alerts_enabled = $4, daily_digest_enabled = $5, digest_hour = $6 
		 WHERE id = $7`,
		fullName, cpf, timezone, emailAlerts, dailyDigest, digestHour, userID,
	)
	if err != nil {
		return fmt.Errorf("UserRepository.UpdateUserProfile: %w", err)
	}
	return nil
}

// UpdateEmailPreferences altera as preferências de notificação do usuário.
func (r *UserRepository) UpdateEmailPreferences(ctx context.Context, userID string, emailAlerts, dailyDigest bool, timezone string, digestHour int) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET email_alerts_enabled = $1, daily_digest_enabled = $2, timezone = $3, digest_hour = $4 WHERE id = $5`,
		emailAlerts, dailyDigest, timezone, digestHour, userID,
	)
	if err != nil {
		return fmt.Errorf("UserRepository.UpdateEmailPreferences: %w", err)
	}
	return nil
}

// UpdateLastDigestSentAt atualiza o timestamp do último envio de resumo diário.
func (r *UserRepository) UpdateLastDigestSentAt(ctx context.Context, userID string, sentAt time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET last_digest_sent_at = $1 WHERE id = $2`,
		sentAt, userID,
	)
	if err != nil {
		return fmt.Errorf("UserRepository.UpdateLastDigestSentAt: %w", err)
	}
	return nil
}

// FindUsersEligibleForDigest busca usuários com daily_digest_enabled ativado.
func (r *UserRepository) FindUsersEligibleForDigest(ctx context.Context) ([]*user.User, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, email, plan, COALESCE(password_hash, ''), COALESCE(full_name, ''), COALESCE(cpf, ''), email_alerts_enabled, daily_digest_enabled, COALESCE(timezone, 'America/Sao_Paulo'), COALESCE(digest_hour, 18), last_digest_sent_at, created_at 
		 FROM users 
		 WHERE daily_digest_enabled = TRUE`)
	if err != nil {
		return nil, fmt.Errorf("UserRepository.FindUsersEligibleForDigest: %w", err)
	}
	defer rows.Close()

	var users []*user.User
	for rows.Next() {
		u := &user.User{}
		err := rows.Scan(&u.ID, &u.Email, &u.Plan, &u.PasswordHash, &u.FullName, &u.CPF, &u.EmailAlertsEnabled, &u.DailyDigestEnabled, &u.Timezone, &u.DigestHour, &u.LastDigestSentAt, &u.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("UserRepository.FindUsersEligibleForDigest scan: %w", err)
		}
		users = append(users, u)
	}
	return users, nil
}

// CountAllJobsByUserID conta todos os jobs criados por um usuário em todos os seus projetos.
func (r *UserRepository) CountAllJobsByUserID(ctx context.Context, userID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs WHERE project_id IN (SELECT id FROM projects WHERE user_id = $1)`,
		userID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("UserRepository.CountAllJobsByUserID: %w", err)
	}
	return count, nil
}

// UpdateProjectName altera o nome de um projeto específico.
func (r *UserRepository) UpdateProjectName(ctx context.Context, projectID, name string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE projects SET name = $1 WHERE id = $2`,
		name, projectID,
	)
	if err != nil {
		return fmt.Errorf("UserRepository.UpdateProjectName: %w", err)
	}
	return nil
}

// DeleteProject remove um projeto por ID.
func (r *UserRepository) DeleteProject(ctx context.Context, projectID string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM projects WHERE id = $1`,
		projectID,
	)
	if err != nil {
		return fmt.Errorf("UserRepository.DeleteProject: %w", err)
	}
	return nil
}

// UpdateVerified atualiza o status de verificação de e-mail do usuário.
func (r *UserRepository) UpdateVerified(ctx context.Context, userID string, verified bool) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET is_verified = $1 WHERE id = $2`,
		verified, userID,
	)
	if err != nil {
		return fmt.Errorf("UserRepository.UpdateVerified: %w", err)
	}
	return nil
}

// ListAllUsersForAdmin lista todos os usuários cadastrados para a área administrativa.
func (r *UserRepository) ListAllUsersForAdmin(ctx context.Context) ([]*user.User, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, email, plan, COALESCE(password_hash, ''), COALESCE(full_name, ''), COALESCE(cpf, ''), email_alerts_enabled, daily_digest_enabled, COALESCE(timezone, 'America/Sao_Paulo'), COALESCE(digest_hour, 18), last_digest_sent_at, is_verified, COALESCE(role, 'user'), COALESCE(ai_queries_used, 0), created_at 
		 FROM users 
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("UserRepository.ListAllUsersForAdmin: %w", err)
	}
	defer rows.Close()

	var users []*user.User
	for rows.Next() {
		u := &user.User{}
		err := rows.Scan(&u.ID, &u.Email, &u.Plan, &u.PasswordHash, &u.FullName, &u.CPF, &u.EmailAlertsEnabled, &u.DailyDigestEnabled, &u.Timezone, &u.DigestHour, &u.LastDigestSentAt, &u.IsVerified, &u.Role, &u.AiQueriesUsed, &u.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("UserRepository.ListAllUsersForAdmin scan: %w", err)
		}
		users = append(users, u)
	}
	return users, nil
}

// IncrementAIQueriesUsed incrementa a cota de uso de IA do usuário no PostgreSQL.
func (r *UserRepository) IncrementAIQueriesUsed(ctx context.Context, userID string) (int, error) {
	var newCount int
	err := r.db.QueryRowContext(ctx,
		`UPDATE users SET ai_queries_used = ai_queries_used + 1 WHERE id = $1 RETURNING ai_queries_used`,
		userID,
	).Scan(&newCount)
	if err != nil {
		return 0, fmt.Errorf("UserRepository.IncrementAIQueriesUsed: %w", err)
	}
	return newCount, nil
}

// ResetAIQueriesUsed reseta a cota de uso de IA do usuário para 0 no PostgreSQL.
func (r *UserRepository) ResetAIQueriesUsed(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET ai_queries_used = 0 WHERE id = $1`, userID)
	if err != nil {
		return fmt.Errorf("UserRepository.ResetAIQueriesUsed: %w", err)
	}
	return nil
}

// UpdateUserPlanByAdmin altera o plano de um usuário diretamente pela administração e atualiza a assinatura.
func (r *UserRepository) UpdateUserPlanByAdmin(ctx context.Context, userID, newPlan string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET plan = $1 WHERE id = $2`,
		newPlan, userID,
	)
	if err != nil {
		return fmt.Errorf("UserRepository.UpdateUserPlanByAdmin: %w", err)
	}

	if newPlan == "pro" {
		_, _ = r.db.ExecContext(ctx, `
			INSERT INTO subscriptions (user_id, plan_code, status, cancel_at_period_end, current_period_start, current_period_end, updated_at)
			VALUES ($1, 'pro', 'active', false, NOW(), NOW() + INTERVAL '30 days', NOW())
			ON CONFLICT (user_id) DO UPDATE SET
				plan_code = 'pro',
				status = 'active',
				cancel_at_period_end = false,
				current_period_end = NOW() + INTERVAL '30 days',
				updated_at = NOW()`,
			userID,
		)
	} else {
		_, _ = r.db.ExecContext(ctx, `
			INSERT INTO subscriptions (user_id, plan_code, status, cancel_at_period_end, updated_at)
			VALUES ($1, 'free', 'active', false, NOW())
			ON CONFLICT (user_id) DO UPDATE SET
				plan_code = 'free',
				status = 'active',
				cancel_at_period_end = false,
				updated_at = NOW()`,
			userID,
		)
		// Pausa jobs excedentes (> 5) ao reverter para Free
		_, _ = r.db.ExecContext(ctx, `
			WITH user_active_jobs AS (
				SELECT j.id,
				       ROW_NUMBER() OVER (ORDER BY j.created_at ASC) as row_num
				FROM jobs j
				JOIN projects p ON j.project_id = p.id
				WHERE p.user_id = $1 AND j.status = 'active'
			)
			UPDATE jobs
			SET status = 'paused', updated_at = NOW()
			WHERE id IN (
				SELECT id FROM user_active_jobs WHERE row_num > 5
			);`, userID)
	}

	return nil
}

// CountTotalPlatformJobs conta o total de jobs ativos na plataforma inteira.
func (r *UserRepository) CountTotalPlatformJobs(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("UserRepository.CountTotalPlatformJobs: %w", err)
	}
	return count, nil
}

// DeleteUserByAdmin remove permanentemente um usuário e todos os seus projetos/jobs associados.
func (r *UserRepository) DeleteUserByAdmin(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		return fmt.Errorf("UserRepository.DeleteUserByAdmin: %w", err)
	}
	return nil
}