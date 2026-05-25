package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/JanGustavo/Cron/internal/auth"
	"github.com/JanGustavo/Cron/internal/config"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
)

type AuthHandler struct {
	userRepo *postgres.UserRepository
	cfg      *config.Config
}

func NewAuthHandler(userRepo *postgres.UserRepository, cfg *config.Config) *AuthHandler {
	return &AuthHandler{userRepo: userRepo, cfg: cfg}
}

type SignupRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	ProjectName string `json:"project_name"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type TokenResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	TokenType    string `json:"tokenType"`
	ExpiresIn    int    `json:"expiresIn"`
}

type ProjectResponse struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
}

type UserResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Plan      string `json:"plan"`
	CreatedAt string `json:"createdAt"`
}

type AuthResponse struct {
	Token    TokenResponse     `json:"token"`
	User     UserResponse      `json:"user"`
	Projects []ProjectResponse `json:"projects"`
	APIKey   string            `json:"apiKey,omitempty"`
}

// Signup — POST /v1/auth/signup
func (h *AuthHandler) Signup(w http.ResponseWriter, r *http.Request) {
	var req SignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "payload inválido")
		return
	}

	if req.Email == "" || req.Password == "" || req.ProjectName == "" {
		writeError(w, http.StatusBadRequest, "todos os campos (email, password, project_name) são obrigatórios")
		return
	}

	// 1. Verifica se usuário já existe
	existingUser, err := h.userRepo.FindByEmail(r.Context(), req.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao verificar email existente")
		return
	}
	if existingUser != nil {
		writeError(w, http.StatusConflict, "este e-mail já está sendo utilizado")
		return
	}

	// 2. Calcula hash da senha
	pwdHash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao processar senha")
		return
	}

	// 3. Cria usuário
	u, err := h.userRepo.CreateUserWithPassword(r.Context(), req.Email, pwdHash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao criar usuário")
		return
	}

	// 4. Cria projeto padrão
	proj, err := h.userRepo.CreateProject(r.Context(), u.ID, req.ProjectName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao criar projeto inicial")
		return
	}

	// 5. Gera chave de API segura vinculada ao projeto
	apiKey, err := auth.Generate()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao gerar chave de API")
		return
	}

	keyHash := auth.Hash(apiKey)
	// Salva apenas o hash no banco
	prefix := apiKey[:12] // Exemplo: "cf_live_abcd"
	if err := h.userRepo.CreateAPIKey(r.Context(), proj.ID, keyHash, prefix); err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao salvar chave de API")
		return
	}

	// 6. Gera token JWT válido por 24 horas
	duration := 24 * time.Hour
	jwtToken, err := auth.GenerateToken(u.ID, u.Email, proj.ID, string(u.Plan), h.cfg.JWTSecret, duration)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao assinar token de autenticação")
		return
	}

	// 7. Retorna resposta
	res := AuthResponse{
		Token: TokenResponse{
			AccessToken:  jwtToken,
			RefreshToken: "",
			TokenType:    "Bearer",
			ExpiresIn:    int(duration.Seconds()),
		},
		User: UserResponse{
			ID:        u.ID,
			Email:     u.Email,
			Plan:      string(u.Plan),
			CreatedAt: u.CreatedAt.Format(time.RFC3339),
		},
		Projects: []ProjectResponse{
			{
				ID:        proj.ID,
				UserID:    proj.UserID,
				Name:      proj.Name,
				CreatedAt: proj.CreatedAt.Format(time.RFC3339),
			},
		},
		APIKey: apiKey, // Plain text mostrada apenas UMA vez no cadastro
	}

	writeJSON(w, http.StatusCreated, res)
}

// Login — POST /v1/auth/login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "payload inválido")
		return
	}

	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email e senha são obrigatórios")
		return
	}

	// 1. Busca usuário
	u, err := h.userRepo.FindByEmail(r.Context(), req.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao buscar usuário")
		return
	}
	if u == nil {
		writeError(w, http.StatusUnauthorized, "credenciais inválidas")
		return
	}

	// 2. Compara hash
	if !auth.CheckPasswordHash(req.Password, u.PasswordHash) {
		writeError(w, http.StatusUnauthorized, "credenciais inválidas")
		return
	}

	// 3. Busca projetos do usuário
	projects, err := h.userRepo.FindProjectsByUserID(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao buscar projetos")
		return
	}

	// Seleciona o ID do projeto ativo (ou default se não houver)
	activeProjectID := "0fe9fb93-3fa0-44b6-b5d8-a5c5b62148a1" // fallback de segurança
	var projResponses []ProjectResponse
	if len(projects) > 0 {
		activeProjectID = projects[0].ID
		for _, p := range projects {
			projResponses = append(projResponses, ProjectResponse{
				ID:        p.ID,
				UserID:    p.UserID,
				Name:      p.Name,
				CreatedAt: p.CreatedAt.Format(time.RFC3339),
			})
		}
	}

	// 4. Gera JWT
	duration := 24 * time.Hour
	jwtToken, err := auth.GenerateToken(u.ID, u.Email, activeProjectID, string(u.Plan), h.cfg.JWTSecret, duration)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao gerar token de autenticação")
		return
	}

	res := AuthResponse{
		Token: TokenResponse{
			AccessToken:  jwtToken,
			RefreshToken: "",
			TokenType:    "Bearer",
			ExpiresIn:    int(duration.Seconds()),
		},
		User: UserResponse{
			ID:        u.ID,
			Email:     u.Email,
			Plan:      string(u.Plan),
			CreatedAt: u.CreatedAt.Format(time.RFC3339),
		},
		Projects: projResponses,
	}

	writeJSON(w, http.StatusOK, res)
}
