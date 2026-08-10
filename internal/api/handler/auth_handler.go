package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/JanGustavo/Cron/internal/api/middleware"
	"github.com/JanGustavo/Cron/internal/auth"
	"github.com/JanGustavo/Cron/internal/config"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/go-chi/chi/v5"
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

// ListAPIKeys — GET /v1/keys
// @Summary Listar API Keys
// @Description Retorna os metadados de todas as chaves de API ativas no projeto (id, prefixo, data de criação). O segredo em texto claro nunca é retornado.
// @Tags API Keys
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {array} postgres.APIKey
// @Router /v1/keys [get]
func (h *AuthHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Não autorizado")
		return
	}

	keys, err := h.userRepo.FindAPIKeysByProjectID(r.Context(), proj.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao listar chaves de API")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

// CreateAPIKey — POST /v1/keys
// @Summary Criar API Key
// @Description Gera uma nova chave de API para o projeto. O segredo em texto claro é retornado apenas nesta chamada. Guarde-o em local seguro.
// @Tags API Keys
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} map[string]string "Nova chave criada"
// @Router /v1/keys [post]
func (h *AuthHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Não autorizado")
		return
	}

	apiKey, err := auth.Generate()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao gerar chave de API")
		return
	}

	keyHash := auth.Hash(apiKey)
	prefix := apiKey[:12] // Exemplo: "cf_live_abcd"
	if err := h.userRepo.CreateAPIKey(r.Context(), proj.ID, keyHash, prefix); err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao salvar nova chave de API")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"apiKey": apiKey,
		"prefix": prefix,
	})
}

// DeleteAPIKey — DELETE /v1/keys/{id}
// @Summary Revogar/Deletar API Key
// @Description Revoga e exclui definitivamente uma chave de API pelo ID. Ela não poderá mais ser usada para autenticação.
// @Tags API Keys
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "ID da chave de API"
// @Success 200 {object} map[string]string "Chave revogada"
// @Router /v1/keys/{id} [delete]
func (h *AuthHandler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Não autorizado")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "ID da chave é obrigatório")
		return
	}

	if err := h.userRepo.DeleteAPIKey(r.Context(), id, proj.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao revogar chave de API: " + err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "Chave de API revogada com sucesso",
	})
}

type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

type ResetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"newPassword"`
}

// ForgotPassword — POST /v1/auth/forgot-password
// @Summary Solicitar redefinição de senha
// @Description Envia um link de recuperação de senha por e-mail (Mockado nos logs do console em desenvolvimento).
// @Tags Autenticação
// @Accept json
// @Produce json
// @Param body body ForgotPasswordRequest true "E-mail do usuário"
// @Success 200 {object} map[string]string "E-mail enviado ou simulado"
// @Router /v1/auth/forgot-password [post]
func (h *AuthHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req ForgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "payload inválido")
		return
	}

	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "o email é obrigatório")
		return
	}

	u, err := h.userRepo.FindByEmail(r.Context(), req.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro interno ao processar solicitação")
		return
	}

	if u == nil {
		// Sucesso genérico para evitar vazamento de e-mails
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "success",
			"message": "Se o e-mail existir em nossa base, um link de recuperação foi enviado.",
		})
		return
	}

	// Gera token temporário de 15 minutos assinado
	token, err := auth.GenerateToken(u.ID, u.Email, "", "", h.cfg.JWTSecret, 15*time.Minute)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao gerar token de recuperação")
		return
	}

	// Mock do envio de e-mail por Logs no terminal
	resetLink := "http://localhost:5173/reset-password?token=" + token
	log.Printf("\n========================================================================\n" +
		"[MOCK EMAIL] Envio de Recuperação de Senha\n" +
		"Para: %s\n" +
		"Assunto: Recuperação de Senha - CronFlow\n" +
		"Link: %s\n" +
		"========================================================================\n", u.Email, resetLink)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "success",
		"message": "E-mail de recuperação enviado (Mock).",
		"link":    resetLink, // Retornamos o link direto na resposta para fins de teste no frontend
	})
}

// ResetPassword — POST /v1/auth/reset-password
// @Summary Redefinir senha com token
// @Description Valida o token recebido por e-mail e atualiza a senha do usuário.
// @Tags Autenticação
// @Accept json
// @Produce json
// @Param body body ResetPasswordRequest true "Token de redefinição e nova senha"
// @Success 200 {object} map[string]string "Senha redefinida com sucesso"
// @Router /v1/auth/reset-password [post]
func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "payload inválido")
		return
	}

	if req.Token == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "token e nova senha são obrigatórios")
		return
	}

	// Valida o token JWT e extrai os claims
	claims, err := auth.ValidateToken(req.Token, h.cfg.JWTSecret)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Token de recuperação inválido ou expirado")
		return
	}

	// Calcula o hash bcrypt da nova senha
	newPwdHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao processar nova senha")
		return
	}

	// Salva a nova senha no banco
	if err := h.userRepo.UpdatePassword(r.Context(), claims.UserID, newPwdHash); err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao atualizar senha no banco")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "Senha redefinida com sucesso!",
	})
}
