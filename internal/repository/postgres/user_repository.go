package postgres

import (
	"context"
	"database/sql"
	"fmt"

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

// CreateUser insere um novo usuário no banco.
func (r *UserRepository) CreateUser(ctx context.Context, email string) (*user.User, error) {
	u := &user.User{}
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO users (email) VALUES ($1) RETURNING id, email, plan, created_at`,
		email,
	).Scan(&u.ID, &u.Email, &u.Plan, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("UserRepository.CreateUser: %w", err)
	}
	return u, nil
}

// CreateProject insere um projeto vinculado a um usuário.
func (r *UserRepository) CreateProject(ctx context.Context, userID, name string) (*project.Project, error) {
	p := &project.Project{}
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO projects (user_id, name) VALUES ($1, $2) RETURNING id, user_id, name, created_at`,
		userID, name,
	).Scan(&p.ID, &p.UserID, &p.Name, &p.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("UserRepository.CreateProject: %w", err)
	}
	return p, nil
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
	return p, nil
}

// FindUserByProjectID retorna o usuário dono de um projeto.
// Usado pelo JobService para verificar o plano e aplicar limites.
func (r *UserRepository) FindUserByProjectID(ctx context.Context, projectID string) (*user.User, error) {
	u := &user.User{}
	err := r.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.plan, u.created_at
		FROM users u
		INNER JOIN projects p ON p.user_id = u.id
		WHERE p.id = $1`,
		projectID,
	).Scan(&u.ID, &u.Email, &u.Plan, &u.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("UserRepository.FindUserByProjectID: %w", err)
	}
	return u, nil
}