package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
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
	FullName    string `json:"full_name"`
	CPF         string `json:"cpf"`
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
	ID            string `json:"id"`
	UserID        string `json:"userId"`
	Name          string `json:"name"`
	CreatedAt     string `json:"createdAt"`
	WebhookSecret string `json:"webhookSecret,omitempty"`
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

var cpfRegexp = regexp.MustCompile(`[^\d]`)

func isValidCPF(cpf string) bool {
	// Remove caracteres não numéricos
	cleanCPF := cpfRegexp.ReplaceAllString(cpf, "")

	if len(cleanCPF) != 11 {
		return false
	}

	// Verifica CPFs conhecidos inválidos
	allSame := true
	for i := 1; i < 11; i++ {
		if cleanCPF[i] != cleanCPF[0] {
			allSame = false
			break
		}
	}
	if allSame {
		return false
	}

	// Valida primeiro dígito verificador
	sum := 0
	for i := 0; i < 9; i++ {
		sum += int(cleanCPF[i]-'0') * (10 - i)
	}
	rest := sum % 11
	d1 := 0
	if rest >= 2 {
		d1 = 11 - rest
	}
	if int(cleanCPF[9]-'0') != d1 {
		return false
	}

	// Valida segundo dígito verificador
	sum = 0
	for i := 0; i < 10; i++ {
		sum += int(cleanCPF[i]-'0') * (11 - i)
	}
	rest = sum % 11
	d2 := 0
	if rest >= 2 {
		d2 = 11 - rest
	}
	if int(cleanCPF[10]-'0') != d2 {
		return false
	}

	return true
}

// Signup — POST /v1/auth/signup
// @Summary Registrar uma nova conta de desenvolvedor
// @Description Cria uma conta de usuário com e-mail, senha, nome completo e CPF, inicializando um projeto padrão e uma chave de API segura.
// @Tags Autenticação
// @Accept json
// @Produce json
// @Param body body SignupRequest true "Dados de cadastro contendo e-mail, senha, nome completo, CPF e nome do projeto inicial"
// @Success 201 {object} AuthResponse "Conta criada, projeto inicializado e chaves geradas"
// @Failure 400 {object} map[string]string "E-mail, senha, nome do projeto, nome completo ou CPF ausentes, ou CPF matematicamente inválido"
// @Failure 409 {object} map[string]string "E-mail ou CPF já cadastrados por outro usuário"
// @Failure 500 {object} map[string]string "Erro interno de banco ou geração de chaves"
// @Router /v1/auth/signup [post]
func (h *AuthHandler) Signup(w http.ResponseWriter, r *http.Request) {
	var req SignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "payload inválido")
		return
	}

	if req.Email == "" || req.Password == "" || req.ProjectName == "" || req.FullName == "" || req.CPF == "" {
		writeError(w, http.StatusBadRequest, "todos os campos (email, password, project_name, full_name, cpf) são obrigatórios")
		return
	}

	// 1. Valida o formato do CPF
	cleanCPF := cpfRegexp.ReplaceAllString(req.CPF, "")
	if !isValidCPF(cleanCPF) {
		writeError(w, http.StatusBadRequest, "CPF inválido. Certifique-se de digitar um CPF real.")
		return
	}

	// 2. Verifica se usuário já existe
	existingUser, err := h.userRepo.FindByEmail(r.Context(), req.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao verificar email existente")
		return
	}
	if existingUser != nil {
		writeError(w, http.StatusConflict, "este e-mail já está sendo utilizado")
		return
	}

	// 3. Verifica se CPF já foi cadastrado para evitar duplicidade de contas (fraude de free-tier)
	existingCPF, err := h.userRepo.FindByCPF(r.Context(), cleanCPF)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao verificar CPF existente")
		return
	}
	if existingCPF != nil {
		writeError(w, http.StatusConflict, "este CPF já está cadastrado em outra conta")
		return
	}

	// 4. Calcula hash da senha
	pwdHash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao processar senha")
		return
	}

	// 5. Cria usuário com CPF
	u, err := h.userRepo.CreateUserWithPassword(r.Context(), req.Email, pwdHash, req.FullName, cleanCPF)
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
				ID:            proj.ID,
				UserID:        proj.UserID,
				Name:          proj.Name,
				CreatedAt:     proj.CreatedAt.Format(time.RFC3339),
				WebhookSecret: auth.ComputeWebhookSecret(proj.ID, h.cfg.JWTSecret),
			},
		},
		APIKey: apiKey, // Plain text mostrada apenas UMA vez no cadastro
	}

	writeJSON(w, http.StatusCreated, res)
}

// Login — POST /v1/auth/login
// @Summary Realizar login
// @Description Autentica um usuário usando e-mail e senha, retornando os tokens JWT e a lista de projetos associados.
// @Tags Autenticação
// @Accept json
// @Produce json
// @Param body body LoginRequest true "Credenciais de login"
// @Success 200 {object} AuthResponse "Autenticado com sucesso"
// @Failure 400 {object} map[string]string "E-mail ou senha ausentes"
// @Failure 401 {object} map[string]string "Credenciais inválidas"
// @Failure 500 {object} map[string]string "Erro interno de autenticação"
// @Router /v1/auth/login [post]
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
				ID:            p.ID,
				UserID:        p.UserID,
				Name:          p.Name,
				CreatedAt:     p.CreatedAt.Format(time.RFC3339),
				WebhookSecret: auth.ComputeWebhookSecret(p.ID, h.cfg.JWTSecret),
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

// OAuthGoogle — GET /v1/auth/oauth/google
func (h *AuthHandler) OAuthGoogle(w http.ResponseWriter, r *http.Request) {
	if h.cfg.GoogleClientID == "" {
		// Mock OAuth Mode
		redirectURL := fmt.Sprintf("%s/v1/auth/oauth/google/callback?code=mock_google_code", h.cfg.APIURL)
		http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
		return
	}

	googleAuthURL := fmt.Sprintf("https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s/v1/auth/oauth/google/callback&response_type=code&scope=openid%%20email%%20profile",
		h.cfg.GoogleClientID, h.cfg.APIURL)
	http.Redirect(w, r, googleAuthURL, http.StatusTemporaryRedirect)
}

// OAuthGoogleCallback — GET /v1/auth/oauth/google/callback
func (h *AuthHandler) OAuthGoogleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, h.cfg.FrontendURL+"/?oauth_error=no_code", http.StatusTemporaryRedirect)
		return
	}

	var email, name string

	if code == "mock_google_code" {
		email = "google_mock_user@example.com"
		name = "Google Developer"
	} else {
		// Real OAuth token exchange
		resp, err := http.PostForm("https://oauth2.googleapis.com/token", url.Values{
			"code":          {code},
			"client_id":     {h.cfg.GoogleClientID},
			"client_secret": {h.cfg.GoogleClientSecret},
			"redirect_uri":  {h.cfg.APIURL + "/v1/auth/oauth/google/callback"},
			"grant_type":    {"authorization_code"},
		})
		if err != nil {
			http.Redirect(w, r, h.cfg.FrontendURL+"/?oauth_error=token_exchange_failed", http.StatusTemporaryRedirect)
			return
		}
		defer resp.Body.Close()

		var tokenResp struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil || tokenResp.AccessToken == "" {
			http.Redirect(w, r, h.cfg.FrontendURL+"/?oauth_error=token_decode_failed", http.StatusTemporaryRedirect)
			return
		}

		// Fetch profile
		req, _ := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
		req.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
		userInfoResp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Redirect(w, r, h.cfg.FrontendURL+"/?oauth_error=userinfo_failed", http.StatusTemporaryRedirect)
			return
		}
		defer userInfoResp.Body.Close()

		var googleUser struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		}
		if err := json.NewDecoder(userInfoResp.Body).Decode(&googleUser); err != nil {
			http.Redirect(w, r, h.cfg.FrontendURL+"/?oauth_error=userinfo_decode_failed", http.StatusTemporaryRedirect)
			return
		}
		email = googleUser.Email
		name = googleUser.Name
	}

	h.handleOAuthUser(w, r, email, name)
}

// OAuthGitHub — GET /v1/auth/oauth/github
func (h *AuthHandler) OAuthGitHub(w http.ResponseWriter, r *http.Request) {
	if h.cfg.GitHubClientID == "" {
		// Mock OAuth Mode
		redirectURL := fmt.Sprintf("%s/v1/auth/oauth/github/callback?code=mock_github_code", h.cfg.APIURL)
		http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
		return
	}

	githubAuthURL := fmt.Sprintf("https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s/v1/auth/oauth/github/callback&scope=user:email",
		h.cfg.GitHubClientID, h.cfg.APIURL)
	http.Redirect(w, r, githubAuthURL, http.StatusTemporaryRedirect)
}

// OAuthGitHubCallback — GET /v1/auth/oauth/github/callback
func (h *AuthHandler) OAuthGitHubCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, h.cfg.FrontendURL+"/?oauth_error=no_code", http.StatusTemporaryRedirect)
		return
	}

	var email, name string

	if code == "mock_github_code" {
		email = "github_mock_user@example.com"
		name = "GitHub Developer"
	} else {
		// Real OAuth token exchange
		req, _ := http.NewRequest("POST", "https://github.com/login/oauth/access_token", strings.NewReader(url.Values{
			"code":          {code},
			"client_id":     {h.cfg.GitHubClientID},
			"client_secret": {h.cfg.GitHubClientSecret},
			"redirect_uri":  {h.cfg.APIURL + "/v1/auth/oauth/github/callback"},
		}.Encode()))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Redirect(w, r, h.cfg.FrontendURL+"/?oauth_error=token_exchange_failed", http.StatusTemporaryRedirect)
			return
		}
		defer resp.Body.Close()

		var tokenResp struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil || tokenResp.AccessToken == "" {
			http.Redirect(w, r, h.cfg.FrontendURL+"/?oauth_error=token_decode_failed", http.StatusTemporaryRedirect)
			return
		}

		// Fetch profile
		reqUser, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
		reqUser.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
		reqUser.Header.Set("Accept", "application/json")
		userResp, err := http.DefaultClient.Do(reqUser)
		if err != nil {
			http.Redirect(w, r, h.cfg.FrontendURL+"/?oauth_error=userinfo_failed", http.StatusTemporaryRedirect)
			return
		}
		defer userResp.Body.Close()

		var githubUser struct {
			Email string `json:"email"`
			Name  string `json:"name"`
			Login string `json:"login"`
		}
		if err := json.NewDecoder(userResp.Body).Decode(&githubUser); err != nil {
			http.Redirect(w, r, h.cfg.FrontendURL+"/?oauth_error=userinfo_decode_failed", http.StatusTemporaryRedirect)
			return
		}

		email = githubUser.Email
		name = githubUser.Name
		if name == "" {
			name = githubUser.Login
		}

		// Fallback for private emails
		if email == "" {
			reqEmails, _ := http.NewRequest("GET", "https://api.github.com/user/emails", nil)
			reqEmails.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
			reqEmails.Header.Set("Accept", "application/json")
			emailsResp, err := http.DefaultClient.Do(reqEmails)
			if err == nil {
				defer emailsResp.Body.Close()
				var githubEmails []struct {
					Email    string `json:"email"`
					Primary  bool   `json:"primary"`
					Verified bool   `json:"verified"`
				}
				if err := json.NewDecoder(emailsResp.Body).Decode(&githubEmails); err == nil {
					for _, ge := range githubEmails {
						if ge.Primary && ge.Verified {
							email = ge.Email
							break
						}
					}
					if email == "" && len(githubEmails) > 0 {
						email = githubEmails[0].Email
					}
				}
			}
		}

		if email == "" {
			email = githubUser.Login + "@github-placeholder.com"
		}
	}

	h.handleOAuthUser(w, r, email, name)
}

// handleOAuthUser localiza ou cria o usuário OAuth e redireciona de volta para o frontend com os tokens JWT correspondentes.
func (h *AuthHandler) handleOAuthUser(w http.ResponseWriter, r *http.Request, email, name string) {
	ctx := r.Context()

	// 1. Busca usuário existente pelo email
	u, err := h.userRepo.FindByEmail(ctx, email)
	if err != nil {
		http.Redirect(w, r, h.cfg.FrontendURL+"/?oauth_error=db_error", http.StatusTemporaryRedirect)
		return
	}

	var projID string
	var apiKey string

	if u == nil {
		// 2. Registra o usuário OAuth (senha em branco e CPF em branco)
		u, err = h.userRepo.CreateUserWithPassword(ctx, email, "", name, "")
		if err != nil {
			http.Redirect(w, r, h.cfg.FrontendURL+"/?oauth_error=create_user_failed", http.StatusTemporaryRedirect)
			return
		}

		// 3. Cria projeto padrão para o usuário OAuth
		proj, err := h.userRepo.CreateProject(ctx, u.ID, "Meu Workspace")
		if err != nil {
			http.Redirect(w, r, h.cfg.FrontendURL+"/?oauth_error=create_project_failed", http.StatusTemporaryRedirect)
			return
		}
		projID = proj.ID

		// 4. Cria chave de API inicial vinculada ao projeto
		keyString, err := auth.Generate()
		if err != nil {
			http.Redirect(w, r, h.cfg.FrontendURL+"/?oauth_error=create_key_failed", http.StatusTemporaryRedirect)
			return
		}
		apiKey = keyString

		keyHash := auth.Hash(apiKey)
		prefix := apiKey[:12]
		if err := h.userRepo.CreateAPIKey(ctx, proj.ID, keyHash, prefix); err != nil {
			http.Redirect(w, r, h.cfg.FrontendURL+"/?oauth_error=save_key_failed", http.StatusTemporaryRedirect)
			return
		}
	} else {
		// Se já existir, busca o primeiro projeto dele
		projs, err := h.userRepo.FindProjectsByUserID(ctx, u.ID)
		if err != nil || len(projs) == 0 {
			proj, err := h.userRepo.CreateProject(ctx, u.ID, "Meu Workspace")
			if err == nil {
				projID = proj.ID
			}
		} else {
			projID = projs[0].ID
		}
	}

	// 5. Gera token JWT válido por 24 horas
	duration := 24 * time.Hour
	jwtToken, err := auth.GenerateToken(u.ID, u.Email, projID, string(u.Plan), h.cfg.JWTSecret, duration)
	if err != nil {
		http.Redirect(w, r, h.cfg.FrontendURL+"/?oauth_error=token_generation_failed", http.StatusTemporaryRedirect)
		return
	}

	// 6. Redireciona o usuário para o frontend carregando os tokens na URL
	redirectURL := fmt.Sprintf("%s/?oauth_token=%s&oauth_user_email=%s&oauth_user_id=%s",
		h.cfg.FrontendURL, url.QueryEscape(jwtToken), url.QueryEscape(u.Email), url.QueryEscape(u.ID))
	
	if apiKey != "" {
		redirectURL += fmt.Sprintf("&oauth_api_key=%s", url.QueryEscape(apiKey))
	}

	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
}
