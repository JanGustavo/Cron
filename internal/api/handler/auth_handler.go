package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/JanGustavo/Cron/internal/service"
	"github.com/go-chi/chi/v5"
)

type AuthHandler struct {
	userRepo          *postgres.UserRepository
	mailService       *service.MailService
	entitlementEngine *service.EntitlementEngine
	cfg               *config.Config
}

func NewAuthHandler(userRepo *postgres.UserRepository, mailService *service.MailService, entitlementEngine *service.EntitlementEngine, cfg *config.Config) *AuthHandler {
	return &AuthHandler{userRepo: userRepo, mailService: mailService, entitlementEngine: entitlementEngine, cfg: cfg}
}

type SignupRequest struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	ProjectName  string `json:"project_name"`
	FullName     string `json:"full_name"`
	CPF          string `json:"cpf"`
	CNPJ         string `json:"cnpj,omitempty"`
	DocumentType string `json:"document_type,omitempty"`
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
	ID        string              `json:"id"`
	Email     string              `json:"email"`
	Plan      string              `json:"plan"`
	FullName  string              `json:"fullName,omitempty"`
	CreatedAt string              `json:"createdAt"`
	Limits    *PlanLimitsResponse `json:"limits,omitempty"`
}

type AuthResponse struct {
	Token    TokenResponse     `json:"token"`
	User     UserResponse      `json:"user"`
	Projects []ProjectResponse `json:"projects"`
	APIKey   string            `json:"apiKey,omitempty"`
}

var cpfRegexp = regexp.MustCompile(`[^\d]`)

func isValidCPF(cpf string) bool {
	cleanCPF := cpfRegexp.ReplaceAllString(cpf, "")

	if len(cleanCPF) != 11 {
		return false
	}

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

func isValidCNPJ(cnpj string) bool {
	cleanCNPJ := cpfRegexp.ReplaceAllString(cnpj, "")

	if len(cleanCNPJ) != 14 {
		return false
	}

	if strings.Repeat(string(cleanCNPJ[0]), 14) == cleanCNPJ {
		return false
	}

	weights1 := []int{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	weights2 := []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}

	sum := 0
	for i := 0; i < 12; i++ {
		sum += int(cleanCNPJ[i]-'0') * weights1[i]
	}
	d1 := sum % 11
	if d1 < 2 {
		d1 = 0
	} else {
		d1 = 11 - d1
	}
	if int(cleanCNPJ[12]-'0') != d1 {
		return false
	}

	sum = 0
	for i := 0; i < 13; i++ {
		sum += int(cleanCNPJ[i]-'0') * weights2[i]
	}
	d2 := sum % 11
	if d2 < 2 {
		d2 = 0
	} else {
		d2 = 11 - d2
	}
	if int(cleanCNPJ[13]-'0') != d2 {
		return false
	}

	return true
}

func isValidTaxDocument(document, documentType string) bool {
	documentType = strings.ToLower(documentType)
	switch documentType {
	case "cnpj":
		return isValidCNPJ(document)
	case "cpf":
		return isValidCPF(document)
	default:
		return false
	}
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
		writeError(w, http.StatusBadRequest, "Payload inválido")
		return
	}

	if req.Email == "" || req.Password == "" || req.ProjectName == "" || req.FullName == "" {
		writeError(w, http.StatusBadRequest, "Todos os campos (email, password, project_name, full_name) são obrigatórios")
		return
	}

	documentType := strings.ToLower(strings.TrimSpace(req.DocumentType))
	if documentType == "" {
		documentType = "cpf"
	}

	documentValue := strings.TrimSpace(req.CPF)
	if documentType == "cnpj" {
		documentValue = strings.TrimSpace(req.CNPJ)
	}
	if documentValue == "" {
		writeError(w, http.StatusBadRequest, "Documento obrigatório: informe CPF ou CNPJ válido.")
		return
	}

	cleanDocument := cpfRegexp.ReplaceAllString(documentValue, "")
	if !isValidTaxDocument(cleanDocument, documentType) {
		if documentType == "cnpj" {
			writeError(w, http.StatusBadRequest, "Cnpj inválido. certifique-se de digitar um cnpj real.")
			return
		}
		writeError(w, http.StatusBadRequest, "Cpf inválido. certifique-se de digitar um cpf real.")
		return
	}

	// 2. Verifica se usuário já existe
	existingUser, err := h.userRepo.FindByEmail(r.Context(), req.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao verificar email existente")
		return
	}
	if existingUser != nil {
		writeError(w, http.StatusConflict, "Este e-mail já está sendo utilizado")
		return
	}

	// 3. Verifica se o documento já foi cadastrado para evitar duplicidade de contas.
	existingCPF, err := h.userRepo.FindByCPF(r.Context(), cleanDocument)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao verificar documento existente")
		return
	}
	if existingCPF != nil {
		if documentType == "cnpj" {
			writeError(w, http.StatusConflict, "Este CNPJ já está cadastrado em outra conta")
			return
		}
		writeError(w, http.StatusConflict, "Este CPF já está cadastrado em outra conta")
		return
	}

	// 4. Calcula hash da senha
	pwdHash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao processar senha")
		return
	}

	// 5. Cria usuário com CPF/CNPJ
	u, err := h.userRepo.CreateUserWithPassword(r.Context(), req.Email, pwdHash, req.FullName, cleanDocument)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao criar usuário")
		return
	}

	// 4. Cria projeto padrão
	_, err = h.userRepo.CreateProject(r.Context(), u.ID, req.ProjectName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao criar projeto inicial")
		return
	}

	// 5. Gera token JWT de verificação válido por 24 horas
	verifyToken, err := auth.GenerateToken(u.ID, u.Email, "", "", h.cfg.JWTSecret, 24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao assinar token de verificação")
		return
	}

	verificationLink := fmt.Sprintf("%s/verify-email?token=%s", h.cfg.FrontendURL, verifyToken)
	go func(email, link string) {
		if err := h.mailService.SendVerificationEmail(email, link); err != nil {
			log.Printf("AuthHandler.Signup (background): falha ao enviar e-mail: %v", err)
		}
	}(u.Email, verificationLink)

	respData := map[string]interface{}{
		"status":                "success",
		"message":               "Conta criada com sucesso! Por favor, verifique seu e-mail para ativar sua conta.",
		"requires_verification": true,
	}
	if h.cfg.AppEnv != "production" {
		respData["link"] = verificationLink
	}
	writeJSON(w, http.StatusCreated, respData)
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
		writeError(w, http.StatusBadRequest, "Payload inválido")
		return
	}

	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "Email e senha são obrigatórios")
		return
	}

	// 1. Busca usuário
	u, err := h.userRepo.FindByEmail(r.Context(), req.Email)
	if err != nil {
		log.Printf("[Auth] Erro ao buscar usuário %s: %v", req.Email, err)
		writeError(w, http.StatusInternalServerError, "Erro ao buscar usuário")
		return
	}
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Credenciais inválidas")
		return
	}

	// 2. Compara hash
	if !auth.CheckPasswordHash(req.Password, u.PasswordHash) {
		writeError(w, http.StatusUnauthorized, "Credenciais inválidas")
		return
	}

	// 2.5. Verifica se o e-mail foi confirmado
	if !u.IsVerified {
		writeError(w, http.StatusForbidden, "Por favor, confirme seu e-mail antes de acessar a conta")
		return
	}

	// 3. Busca projetos do usuário
	projects, err := h.userRepo.FindProjectsByUserID(r.Context(), u.ID)
	if err != nil {
		log.Printf("[Auth] Erro ao buscar projetos para usuário %s (%s): %v", u.Email, u.ID, err)
		writeError(w, http.StatusInternalServerError, "Erro ao buscar projetos")
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
				WebhookSecret: func() string {
					if p.WebhookSecret != nil && *p.WebhookSecret != "" {
						return *p.WebhookSecret
					}
					return auth.ComputeWebhookSecret(p.ID, h.cfg.JWTSecret)
				}(),
			})
		}
	}

	// 4. Gera JWT
	duration := 24 * time.Hour
	jwtToken, err := auth.GenerateToken(u.ID, u.Email, activeProjectID, string(u.Plan), h.cfg.JWTSecret, duration)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao gerar token de autenticação")
		return
	}

	limits, err := h.entitlementEngine.GetUserLimits(r.Context(), u.ID)
	if err != nil {
		log.Printf("[Auth] Erro ao buscar limites para usuário %s (%s): %v", u.Email, u.ID, err)
		writeError(w, http.StatusInternalServerError, "Erro ao obter limites do plano")
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
			FullName:  u.FullName,
			CreatedAt: u.CreatedAt.Format(time.RFC3339),
			Limits: &PlanLimitsResponse{
				MaxJobs:               limits.MaxJobs,
				MaxUsers:              limits.MaxUsers,
				LogsRetentionDays:     limits.LogsRetentionDays,
				WorkflowsEnabled:      limits.WorkflowsEnabled,
				AlertsWebhooksEnabled: limits.AlertsWebhooksEnabled,
				MultiProjectEnabled:   limits.MultiProjectEnabled,
			},
		},
		Projects: projResponses,
	}

	writeJSON(w, http.StatusOK, res)
}

type VerifyEmailRequest struct {
	Token string `json:"token"`
}

// VerifyEmail — POST /v1/auth/verify-email
// @Summary Confirmar cadastro e ativar conta
// @Description Confirma o e-mail do usuário usando o token enviado por e-mail e ativa a conta gerando a chave de API inicial.
// @Tags Autenticação
// @Accept json
// @Produce json
// @Param body body VerifyEmailRequest true "Token de confirmação de e-mail"
// @Success 200 {object} AuthResponse "Conta ativada e autenticada"
// @Router /v1/auth/verify-email [post]
func (h *AuthHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req VerifyEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Payload inválido")
		return
	}

	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "O token de confirmação é obrigatório")
		return
	}

	// 1. Valida o token JWT e extrai os claims
	claims, err := auth.ValidateToken(req.Token, h.cfg.JWTSecret)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Token de confirmação inválido ou expirado")
		return
	}

	// 2. Busca o usuário
	u, err := h.userRepo.FindByEmail(r.Context(), claims.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao buscar usuário")
		return
	}
	if u == nil {
		writeError(w, http.StatusNotFound, "Usuário não encontrado")
		return
	}

	// 3. Busca projetos do usuário
	projects, err := h.userRepo.FindProjectsByUserID(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao buscar projetos")
		return
	}

	if len(projects) == 0 {
		_, err = h.userRepo.CreateProject(r.Context(), u.ID, "Projeto Principal")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Erro ao criar projeto")
			return
		}
		// Recarrega os projetos
		projects, err = h.userRepo.FindProjectsByUserID(r.Context(), u.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Erro ao buscar projetos")
			return
		}
	}

	var apiKey string

	// 4. Se não estiver verificado, ativa e gera a primeira chave de API
	if !u.IsVerified {
		if err := h.userRepo.UpdateVerified(r.Context(), u.ID, true); err != nil {
			writeError(w, http.StatusInternalServerError, "Erro ao ativar usuário")
			return
		}

		apiKey, err = auth.Generate()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Erro ao gerar chave de API")
			return
		}

		keyHash := auth.Hash(apiKey)
		prefix := apiKey[:12]
		if err := h.userRepo.CreateAPIKey(r.Context(), projects[0].ID, keyHash, prefix); err != nil {
			writeError(w, http.StatusInternalServerError, "Erro ao salvar chave de API")
			return
		}
	}

	// 5. Gera token JWT válido por 24 horas para o login imediato
	duration := 24 * time.Hour
	jwtToken, err := auth.GenerateToken(u.ID, u.Email, projects[0].ID, string(u.Plan), h.cfg.JWTSecret, duration)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao assinar token de autenticação")
		return
	}

	var projResponses []ProjectResponse
	for _, p := range projects {
		projResponses = append(projResponses, ProjectResponse{
			ID:        p.ID,
			UserID:    p.UserID,
			Name:      p.Name,
			CreatedAt: p.CreatedAt.Format(time.RFC3339),
			WebhookSecret: func() string {
				if p.WebhookSecret != nil && *p.WebhookSecret != "" {
					return *p.WebhookSecret
				}
				return auth.ComputeWebhookSecret(p.ID, h.cfg.JWTSecret)
			}(),
		})
	}

	limits, err := h.entitlementEngine.GetUserLimits(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao obter limites do plano")
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
			FullName:  u.FullName,
			CreatedAt: u.CreatedAt.Format(time.RFC3339),
			Limits: &PlanLimitsResponse{
				MaxJobs:               limits.MaxJobs,
				MaxUsers:              limits.MaxUsers,
				LogsRetentionDays:     limits.LogsRetentionDays,
				WorkflowsEnabled:      limits.WorkflowsEnabled,
				AlertsWebhooksEnabled: limits.AlertsWebhooksEnabled,
				MultiProjectEnabled:   limits.MultiProjectEnabled,
			},
		},
		Projects: projResponses,
		APIKey:   apiKey,
	}

	writeJSON(w, http.StatusOK, res)
}

type ResendVerificationRequest struct {
	Email string `json:"email"`
}

// ResendVerification — POST /v1/auth/resend-verification
// @Summary Reenviar e-mail de confirmação
// @Description Reenvia o link de ativação da conta para o e-mail informado.
// @Tags Autenticação
// @Accept json
// @Produce json
// @Param body body ResendVerificationRequest true "E-mail cadastrado"
// @Success 200 {object} map[string]string "E-mail reenviado com sucesso"
// @Router /v1/auth/resend-verification [post]
func (h *AuthHandler) ResendVerification(w http.ResponseWriter, r *http.Request) {
	var req ResendVerificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Payload inválido")
		return
	}

	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "O e-mail é obrigatório")
		return
	}

	// 1. Busca o usuário
	u, err := h.userRepo.FindByEmail(r.Context(), req.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao buscar usuário")
		return
	}
	if u == nil {
		writeError(w, http.StatusNotFound, "Este e-mail não está cadastrado")
		return
	}

	// 2. Se já verificado, avisa
	if u.IsVerified {
		writeError(w, http.StatusConflict, "Este e-mail já foi verificado e a conta está ativa")
		return
	}

	// 3. Gera novo token de verificação
	verifyToken, err := auth.GenerateToken(u.ID, u.Email, "", "", h.cfg.JWTSecret, 24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao gerar token de verificação")
		return
	}

	verificationLink := fmt.Sprintf("%s/verify-email?token=%s", h.cfg.FrontendURL, verifyToken)
	go func(email, link string) {
		if err := h.mailService.SendVerificationEmail(email, link); err != nil {
			log.Printf("AuthHandler.ResendVerification (background): falha ao enviar e-mail: %v", err)
		}
	}(u.Email, verificationLink)

	respData := map[string]interface{}{
		"status":                "success",
		"message":               "Link de confirmação reenviado com sucesso!",
		"requires_verification": true,
	}
	if h.cfg.AppEnv != "production" {
		respData["link"] = verificationLink
	}
	writeJSON(w, http.StatusOK, respData)
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
		writeError(w, http.StatusInternalServerError, "Erro ao listar chaves de api")
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
// @Router /v1/keys [post]
func (h *AuthHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Não autorizado")
		return
	}

	apiKey, err := auth.Generate()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao gerar chave de api")
		return
	}

	keyHash := auth.Hash(apiKey)
	prefix := apiKey[:12] // Exemplo: "cf_live_abcd"
	if err := h.userRepo.CreateAPIKey(r.Context(), proj.ID, keyHash, prefix); err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao salvar nova chave de api")
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
		writeError(w, http.StatusBadRequest, "Id da chave é obrigatório")
		return
	}

	if err := h.userRepo.DeleteAPIKey(r.Context(), id, proj.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao revogar chave de api: "+err.Error())
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
		writeError(w, http.StatusBadRequest, "Payload inválido")
		return
	}

	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "O email é obrigatório")
		return
	}

	u, err := h.userRepo.FindByEmail(r.Context(), req.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro interno ao processar solicitação")
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
		writeError(w, http.StatusInternalServerError, "Erro ao gerar token de recuperação")
		return
	}

	resetLink := fmt.Sprintf("%s/reset-password?token=%s", h.cfg.FrontendURL, token)
	go func(email, link string) {
		if err := h.mailService.SendPasswordResetEmail(email, link); err != nil {
			log.Printf("AuthHandler.ForgotPassword (background): falha ao enviar e-mail: %v", err)
		}
	}(u.Email, resetLink)

	respData := map[string]interface{}{
		"status":  "success",
		"message": "Instruções de recuperação enviadas.",
	}
	if h.cfg.AppEnv != "production" {
		respData["link"] = resetLink
	}
	writeJSON(w, http.StatusOK, respData)
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
		writeError(w, http.StatusBadRequest, "Payload inválido")
		return
	}

	if req.Token == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "Token e nova senha são obrigatórios")
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
		writeError(w, http.StatusInternalServerError, "Erro ao processar nova senha")
		return
	}

	// Salva a nova senha no banco
	if err := h.userRepo.UpdatePassword(r.Context(), claims.UserID, newPwdHash); err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao atualizar senha no banco")
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
			log.Printf("[OAuth Google] Erro na requisição de token: %v", err)
			http.Redirect(w, r, h.cfg.FrontendURL+"/?oauth_error=token_exchange_failed", http.StatusTemporaryRedirect)
			return
		}
		defer resp.Body.Close()

		bodyBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			log.Printf("[OAuth Google] Erro ao ler resposta do token: %v", readErr)
			http.Redirect(w, r, h.cfg.FrontendURL+"/?oauth_error=token_decode_failed", http.StatusTemporaryRedirect)
			return
		}

		var tokenResp struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil || tokenResp.AccessToken == "" {
			log.Printf("[OAuth Google] Falha ao decodificar token. Status: %d, Body: %s", resp.StatusCode, string(bodyBytes))
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

// RotateWebhookSecret — POST /v1/projects/webhook-secret/rotate
// @Summary Rotacionar Segredo de Webhook
// @Description Gera e salva um novo segredo HMAC-SHA256 aleatório para assinar os webhooks de alerta do workspace.
// @Tags Autenticação
// @Produce json
// @Success 200 {object} map[string]string "Novo segredo gerado"
// @Failure 401 {object} map[string]string "Não autenticado"
// @Failure 500 {object} map[string]string "Erro interno"
// @Security ApiKeyAuth
// @Router /v1/projects/webhook-secret/rotate [post]
func (h *AuthHandler) RotateWebhookSecret(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Não autorizado")
		return
	}

	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao gerar segredo aleatório")
		return
	}
	newSecret := "whsec_" + hex.EncodeToString(bytes)

	if err := h.userRepo.UpdateProjectWebhookSecret(r.Context(), proj.ID, newSecret); err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao salvar novo segredo no banco")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"webhookSecret": newSecret,
	})
}

type UpdateProfileRequest struct {
	EmailAlertsEnabled bool   `json:"email_alerts_enabled"`
	DailyDigestEnabled bool   `json:"daily_digest_enabled"`
	Timezone           string `json:"timezone"`
	DigestHour         int    `json:"digest_hour"`
}

type PlanLimitsResponse struct {
	MaxJobs               int  `json:"maxJobs"`
	MaxUsers              int  `json:"maxUsers"`
	LogsRetentionDays     int  `json:"logsRetentionDays"`
	WorkflowsEnabled      bool `json:"workflowsEnabled"`
	AlertsWebhooksEnabled bool `json:"alertsWebhooksEnabled"`
	MultiProjectEnabled   bool `json:"multiProjectEnabled"`
}

type ProfileResponse struct {
	ID                 string             `json:"id"`
	Email              string             `json:"email"`
	Plan               string             `json:"plan"`
	FullName           string             `json:"fullName"`
	CPF                string             `json:"cpf"`
	EmailAlertsEnabled bool               `json:"emailAlertsEnabled"`
	DailyDigestEnabled bool               `json:"dailyDigestEnabled"`
	Timezone           string             `json:"timezone"`
	DigestHour         int                `json:"digestHour"`
	CreatedAt          string             `json:"createdAt"`
	TotalJobsCreated   int                `json:"totalJobsCreated"`
	Projects           []ProjectResponse  `json:"projects"`
	Limits             PlanLimitsResponse `json:"limits"`
	AiQueriesUsed      int                `json:"aiQueriesUsed"`
	CurrentPeriodEnd   *string            `json:"currentPeriodEnd,omitempty"`
}

// GetProfile — GET /v1/users/profile
// @Summary Obter perfil do usuário
// @Description Retorna o perfil completo do desenvolvedor logado, incluindo preferências de e-mail e daily digest.
// @Tags Usuário
// @Produce json
// @Success 200 {object} ProfileResponse "Perfil carregado com sucesso"
// @Failure 401 {object} map[string]string "Não autorizado"
// @Failure 500 {object} map[string]string "Erro interno"
// @Security ApiKeyAuth
// @Router /v1/users/profile [get]
func (h *AuthHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Não autorizado")
		return
	}

	u, err := h.userRepo.FindUserByProjectID(r.Context(), proj.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao obter perfil do usuário")
		return
	}
	if u == nil {
		writeError(w, http.StatusNotFound, "Usuário não encontrado")
		return
	}

	totalJobsCreated, err := h.userRepo.CountAllJobsByUserID(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao contar jobs do usuário")
		return
	}

	userProjects, err := h.userRepo.FindProjectsByUserID(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao obter projetos do usuário")
		return
	}

	projsResp := make([]ProjectResponse, len(userProjects))
	for i, p := range userProjects {
		webhookSec := ""
		if p.WebhookSecret != nil {
			webhookSec = *p.WebhookSecret
		} else {
			webhookSec = auth.ComputeWebhookSecret(p.ID, h.cfg.JWTSecret)
		}

		projsResp[i] = ProjectResponse{
			ID:            p.ID,
			UserID:        p.UserID,
			Name:          p.Name,
			CreatedAt:     p.CreatedAt.Format(time.RFC3339),
			WebhookSecret: webhookSec,
		}
	}

	limits, err := h.entitlementEngine.GetUserLimits(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao obter limites do plano do usuário")
		return
	}

	subInfo, _ := h.entitlementEngine.GetSubscription(r.Context(), u.ID)
	var expiryStr *string
	if subInfo != nil && subInfo.CurrentPeriodEnd != nil {
		formatted := subInfo.CurrentPeriodEnd.Format(time.RFC3339)
		expiryStr = &formatted
	}

	resp := ProfileResponse{
		ID:                 u.ID,
		Email:              u.Email,
		Plan:               string(u.Plan),
		FullName:           u.FullName,
		CPF:                u.CPF,
		EmailAlertsEnabled: u.EmailAlertsEnabled,
		DailyDigestEnabled: u.DailyDigestEnabled,
		Timezone:           u.Timezone,
		DigestHour:         u.DigestHour,
		CreatedAt:          u.CreatedAt.Format(time.RFC3339),
		TotalJobsCreated:   totalJobsCreated,
		Projects:           projsResp,
		Limits: PlanLimitsResponse{
			MaxJobs:               limits.MaxJobs,
			MaxUsers:              limits.MaxUsers,
			LogsRetentionDays:     limits.LogsRetentionDays,
			WorkflowsEnabled:      limits.WorkflowsEnabled,
			AlertsWebhooksEnabled: limits.AlertsWebhooksEnabled,
			MultiProjectEnabled:   limits.MultiProjectEnabled,
		},
		AiQueriesUsed:    u.AiQueriesUsed,
		CurrentPeriodEnd: expiryStr,
	}

	writeJSON(w, http.StatusOK, resp)
}

// UpdateProfile — PUT /v1/users/profile
// @Summary Atualizar preferências do usuário
// @Description Atualiza as configurações de notificação de e-mail e daily digest do usuário.
// @Tags Usuário
// @Accept json
// @Produce json
// @Param body body UpdateProfileRequest true "Preferências a serem atualizadas"
// @Success 200 {object} map[string]string "Preferências salvas com sucesso"
// @Failure 401 {object} map[string]string "Não autorizado"
// @Failure 500 {object} map[string]string "Erro interno"
// @Security ApiKeyAuth
// @Router /v1/users/profile [put]
func (h *AuthHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Não autorizado")
		return
	}

	var req UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Corpo da requisição inválido")
		return
	}

	// Se a timezone estiver vazia, define padrão
	if req.Timezone == "" {
		req.Timezone = "America/Sao_Paulo"
	}
	// Valida se timezone existe
	if _, err := time.LoadLocation(req.Timezone); err != nil {
		writeError(w, http.StatusBadRequest, "Timezone inválida")
		return
	}
	// Valida hora do digest
	if req.DigestHour < 0 || req.DigestHour > 23 {
		writeError(w, http.StatusBadRequest, "Digest_hour deve ser entre 0 e 23")
		return
	}

	// Busca o usuário para verificar
	u, err := h.userRepo.FindUserByProjectID(r.Context(), proj.ID)
	if err != nil || u == nil {
		writeError(w, http.StatusNotFound, "Usuário não encontrado")
		return
	}

	// Se for plano free, não pode ativar email_alerts_enabled (alertas imediatos)
	if u.Plan == "free" && req.EmailAlertsEnabled {
		writeError(w, http.StatusBadRequest, "Alertas imediatos por e-mail estão disponíveis apenas no plano PRO")
		return
	}

	err = h.userRepo.UpdateEmailPreferences(r.Context(), u.ID, req.EmailAlertsEnabled, req.DailyDigestEnabled, req.Timezone, req.DigestHour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao atualizar preferências")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "preferências atualizadas com sucesso",
	})
}

type CreateProjectRequest struct {
	Name string `json:"name"`
}

// CreateProject — POST /v1/projects
// @Summary Criar novo projeto (Workspace)
// @Description Cria um novo workspace/projeto para o usuário autenticado. Apenas usuários do plano PRO/Pago podem possuir múltiplos projetos.
// @Tags Projetos
// @Accept json
// @Produce json
// @Param body body CreateProjectRequest true "Dados do projeto"
// @Success 201 {object} map[string]any "Projeto criado com sucesso"
// @Failure 400 {object} map[string]string "Dados inválidos"
// @Failure 401 {object} map[string]string "Não autorizado"
// @Failure 403 {object} map[string]string "Limite de projetos atingido"
// @Failure 500 {object} map[string]string "Erro interno"
// @Security ApiKeyAuth
// @Router /v1/projects [post]
func (h *AuthHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	projContext := middleware.ProjectFromContext(r.Context())
	if projContext == nil {
		writeError(w, http.StatusUnauthorized, "Não autorizado")
		return
	}

	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Corpo da requisição inválido")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "O nome do projeto não pode ser vazio")
		return
	}

	// Busca o usuário correspondente para validar o plano
	u, err := h.userRepo.FindUserByProjectID(r.Context(), projContext.ID)
	if err != nil || u == nil {
		writeError(w, http.StatusNotFound, "Usuário não encontrado")
		return
	}

	// Limita os projetos conforme as regras dinâmicas do plano
	projects, err := h.userRepo.FindProjectsByUserID(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao verificar projetos existentes")
		return
	}

	limits, err := h.entitlementEngine.GetUserLimits(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao verificar limites do plano")
		return
	}

	if !limits.MultiProjectEnabled {
		if len(projects) >= 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Apenas usuários do plano PRO podem criar múltiplos projetos (workspaces). Faça o upgrade da sua conta para continuar!",
				"code":  "LIMIT_EXCEEDED",
			})
			return
		}
	} else {
		if len(projects) >= 5 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Limite de 5 projetos (workspaces) atingido para o plano PRO.",
				"code":  "LIMIT_EXCEEDED",
			})
			return
		}
	}

	newProj, err := h.userRepo.CreateProject(r.Context(), u.ID, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao criar projeto")
		return
	}

	writeJSON(w, http.StatusCreated, newProj)
}

type UpdateProjectRequest struct {
	Name string `json:"name"`
}

// UpdateProject — PUT /v1/projects/{id}
func (h *AuthHandler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	projContext := middleware.ProjectFromContext(r.Context())
	if projContext == nil {
		writeError(w, http.StatusUnauthorized, "Não autorizado")
		return
	}

	projID := chi.URLParam(r, "id")
	if projID == "" {
		writeError(w, http.StatusBadRequest, "Id do projeto é obrigatório")
		return
	}

	var req UpdateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Corpo da requisição inválido")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "O nome do projeto não pode ser vazio")
		return
	}

	// Busca o usuário logado
	u, err := h.userRepo.FindUserByProjectID(r.Context(), projContext.ID)
	if err != nil || u == nil {
		writeError(w, http.StatusNotFound, "Usuário não encontrado")
		return
	}

	// Busca o dono do projeto a ser editado
	owner, err := h.userRepo.FindUserByProjectID(r.Context(), projID)
	if err != nil || owner == nil {
		writeError(w, http.StatusNotFound, "Projeto não encontrado")
		return
	}

	if owner.ID != u.ID {
		writeError(w, http.StatusForbidden, "Acesso negado")
		return
	}

	err = h.userRepo.UpdateProjectName(r.Context(), projID, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao atualizar projeto")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "projeto atualizado com sucesso",
	})
}

// DeleteProject — DELETE /v1/projects/{id}
func (h *AuthHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	projContext := middleware.ProjectFromContext(r.Context())
	if projContext == nil {
		writeError(w, http.StatusUnauthorized, "Não autorizado")
		return
	}

	projID := chi.URLParam(r, "id")
	if projID == "" {
		writeError(w, http.StatusBadRequest, "Id do projeto é obrigatório")
		return
	}

	// Busca o usuário logado
	u, err := h.userRepo.FindUserByProjectID(r.Context(), projContext.ID)
	if err != nil || u == nil {
		writeError(w, http.StatusNotFound, "Usuário não encontrado")
		return
	}

	// Busca o dono do projeto a ser deletado
	owner, err := h.userRepo.FindUserByProjectID(r.Context(), projID)
	if err != nil || owner == nil {
		writeError(w, http.StatusNotFound, "Projeto não encontrado")
		return
	}

	if owner.ID != u.ID {
		writeError(w, http.StatusForbidden, "Acesso negado")
		return
	}

	// Não deixa excluir o último projeto do usuário
	projects, err := h.userRepo.FindProjectsByUserID(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao verificar projetos")
		return
	}
	if len(projects) <= 1 {
		writeError(w, http.StatusForbidden, "Não é possível excluir seu único projeto ativo")
		return
	}

	err = h.userRepo.DeleteProject(r.Context(), projID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao excluir projeto")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "projeto excluído com sucesso",
	})
}

// SwitchProject — POST /v1/projects/{id}/switch
// @Summary Alternar projeto ativo
// @Description Retorna um novo token JWT assinado para o projeto solicitado, validando que pertence ao mesmo usuário.
// @Tags Projetos
// @Produce json
// @Param id path string true "ID do projeto de destino"
// @Success 200 {object} map[string]string "Novo token gerado"
// @Failure 400 {object} map[string]string "ID inválido"
// @Failure 401 {object} map[string]string "Não autorizado"
// @Failure 403 {object} map[string]string "Acesso negado ao projeto"
// @Failure 500 {object} map[string]string "Erro interno"
// @Security ApiKeyAuth
// @Router /v1/projects/{id}/switch [post]
func (h *AuthHandler) SwitchProject(w http.ResponseWriter, r *http.Request) {
	projContext := middleware.ProjectFromContext(r.Context())
	if projContext == nil {
		writeError(w, http.StatusUnauthorized, "Não autorizado")
		return
	}

	projID := chi.URLParam(r, "id")
	if projID == "" {
		writeError(w, http.StatusBadRequest, "Id do projeto é obrigatório")
		return
	}

	// Busca o usuário logado
	u, err := h.userRepo.FindUserByProjectID(r.Context(), projContext.ID)
	if err != nil || u == nil {
		writeError(w, http.StatusNotFound, "Usuário não encontrado")
		return
	}

	// Busca o dono do projeto a ser alternado
	owner, err := h.userRepo.FindUserByProjectID(r.Context(), projID)
	if err != nil || owner == nil {
		writeError(w, http.StatusNotFound, "Projeto não encontrado")
		return
	}

	if owner.ID != u.ID {
		writeError(w, http.StatusForbidden, "Acesso negado")
		return
	}

	// Gera um novo token JWT com o novo projID
	duration := 24 * time.Hour
	jwtToken, err := auth.GenerateToken(u.ID, u.Email, projID, string(u.Plan), h.cfg.JWTSecret, duration)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao gerar novo token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"token": jwtToken,
	})
}
