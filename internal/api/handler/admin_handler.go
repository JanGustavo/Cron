package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/JanGustavo/Cron/internal/api/middleware"
	"github.com/JanGustavo/Cron/internal/config"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/redis/go-redis/v9"
)

type AdminHandler struct {
	userRepo    *postgres.UserRepository
	billingRepo *postgres.BillingRepository
	redis       *redis.Client
	cfg         *config.Config
}

func NewAdminHandler(userRepo *postgres.UserRepository, billingRepo *postgres.BillingRepository, redisClient *redis.Client, cfg *config.Config) *AdminHandler {
	return &AdminHandler{
		userRepo:    userRepo,
		billingRepo: billingRepo,
		redis:       redisClient,
		cfg:         cfg,
	}
}

type AdminUserDTO struct {
	ID                     string     `json:"id"`
	Email                  string     `json:"email"`
	FullName               string     `json:"fullName"`
	Plan                   string     `json:"plan"`
	Role                   string     `json:"role"`
	IsVerified             bool       `json:"isVerified"`
	TotalJobs              int        `json:"totalJobs"`
	AiQueriesUsed          int        `json:"aiQueriesUsed"`
	BillingCycle           string     `json:"billingCycle"`       // "monthly" | "yearly" | "none"
	CurrentPeriodEnd       *time.Time `json:"currentPeriodEnd"`   // Data limite do plano
	CurrentPeriodStart     *time.Time `json:"currentPeriodStart"` // Data de início
	SubscriptionStatus     string     `json:"subscriptionStatus"` // "active" | "canceled" | "none"
	BillingProvider        string     `json:"billingProvider"`    // "asaas" | "manual" | "none"
	ProviderCustomerID     *string    `json:"providerCustomerId,omitempty"`
	ProviderSubscriptionID *string    `json:"providerSubscriptionId,omitempty"`
	AsaasURL               string     `json:"asaasUrl,omitempty"`
	CreatedAt              time.Time  `json:"createdAt"`
}

// ListUsers — GET /v1/admin/users
func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.userRepo.ListAllUsersForAdmin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Erro ao listar usuários: %v", err))
		return
	}

	var asaasBaseURL = "https://sandbox.asaas.com"
	if h.cfg != nil {
		if strings.HasPrefix(h.cfg.AsaasAPIKey, "$aact_hmlg_") {
			asaasBaseURL = "https://sandbox.asaas.com"
		} else if h.cfg.AppEnv == "production" {
			asaasBaseURL = "https://www.asaas.com"
		}
	}

	var dtos []AdminUserDTO
	for _, u := range users {
		jobCount, _ := h.userRepo.CountAllJobsByUserID(r.Context(), u.ID)

		aiUsed := u.AiQueriesUsed
		if aiUsed == 0 && h.redis != nil {
			redisKey := fmt.Sprintf("ai_usage:%s", u.ID)
			val, _ := h.redis.Get(r.Context(), redisKey).Int()
			if val > 0 {
				aiUsed = val
			}
		}

		var billingCycle = "none"
		var currentPeriodEnd *time.Time
		var currentPeriodStart *time.Time
		var subStatus = "none"
		var billingProvider = "none"
		var providerCustID *string
		var providerSubID *string
		var asaasURL = ""

		if h.billingRepo != nil {
			sub, _ := h.billingRepo.GetSubscription(r.Context(), u.ID)
			if sub != nil {
				subStatus = sub.Status
				billingProvider = sub.BillingProvider
				if billingProvider == "" && u.Plan == "pro" {
					billingProvider = "manual"
				}
				currentPeriodEnd = sub.CurrentPeriodEnd
				currentPeriodStart = sub.CurrentPeriodStart
				providerCustID = sub.ProviderCustomerID
				providerSubID = sub.ProviderSubscriptionID

				if sub.ProviderSubscriptionID != nil && *sub.ProviderSubscriptionID != "" {
					asaasURL = fmt.Sprintf("%s/subscriptions", asaasBaseURL)
				} else if sub.ProviderCustomerID != nil && *sub.ProviderCustomerID != "" {
					asaasURL = fmt.Sprintf("%s/subscriptions", asaasBaseURL)
				}

				if sub.CurrentPeriodStart != nil && sub.CurrentPeriodEnd != nil {
					days := sub.CurrentPeriodEnd.Sub(*sub.CurrentPeriodStart).Hours() / 24
					if days > 60 {
						billingCycle = "yearly"
					} else {
						billingCycle = "monthly"
					}
				} else if u.Plan == "pro" {
					billingCycle = "monthly"
				}
			} else if u.Plan == "pro" {
				subStatus = "active"
				billingCycle = "monthly"
				billingProvider = "manual"
			}
		}

		dtos = append(dtos, AdminUserDTO{
			ID:                     u.ID,
			Email:                  u.Email,
			FullName:               u.FullName,
			Plan:                   string(u.Plan),
			Role:                   u.Role,
			IsVerified:             u.IsVerified,
			TotalJobs:              jobCount,
			AiQueriesUsed:          aiUsed,
			BillingCycle:           billingCycle,
			CurrentPeriodEnd:       currentPeriodEnd,
			CurrentPeriodStart:     currentPeriodStart,
			SubscriptionStatus:     subStatus,
			BillingProvider:        billingProvider,
			ProviderCustomerID:     providerCustID,
			ProviderSubscriptionID: providerSubID,
			AsaasURL:               asaasURL,
			CreatedAt:              u.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"users":  dtos,
		"count":  len(dtos),
	})
}

type UpdatePlanRequest struct {
	Plan string `json:"plan"` // "free" ou "pro"
}

// UpdateUserPlan — PUT /v1/admin/users/{id}/plan
func (h *AdminHandler) UpdateUserPlan(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		writeError(w, http.StatusBadRequest, "ID do usuário não especificado")
		return
	}
	targetUserID := pathParts[4]

	var req UpdatePlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Corpo da requisição inválido")
		return
	}

	newPlan := strings.ToLower(strings.TrimSpace(req.Plan))
	if newPlan != "free" && newPlan != "pro" {
		writeError(w, http.StatusBadRequest, "Plano inválido. Use 'free' ou 'pro'")
		return
	}

	err := h.userRepo.UpdateUserPlanByAdmin(r.Context(), targetUserID, newPlan)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Erro ao atualizar plano: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"message": fmt.Sprintf("Plano alterado para '%s' com sucesso", newPlan),
		"userId":  targetUserID,
		"plan":    newPlan,
	})
}

// ResetUserAIQuota — POST /v1/admin/users/{id}/reset-ai
func (h *AdminHandler) ResetUserAIQuota(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		writeError(w, http.StatusBadRequest, "ID do usuário não especificado")
		return
	}
	targetUserID := pathParts[4]

	_ = h.userRepo.ResetAIQueriesUsed(r.Context(), targetUserID)

	if h.redis != nil {
		redisKey := fmt.Sprintf("ai_usage:%s", targetUserID)
		_ = h.redis.Del(r.Context(), redisKey).Err()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"message": "Cota de uso da IA resetada para 0 com sucesso (3 testes grátis renovados)",
		"userId":  targetUserID,
	})
}

type ToggleVerifyRequest struct {
	Verified bool `json:"verified"`
}

// VerifyUserEmail — POST /v1/admin/users/{id}/verify
func (h *AdminHandler) VerifyUserEmail(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		writeError(w, http.StatusBadRequest, "ID do usuário não especificado")
		return
	}
	targetUserID := pathParts[4]

	var req ToggleVerifyRequest
	req.Verified = true
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	err := h.userRepo.UpdateVerified(r.Context(), targetUserID, req.Verified)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Erro ao atualizar verificação de e-mail: %v", err))
		return
	}

	statusMsg := "e-mail marcado como verificado"
	if !req.Verified {
		statusMsg = "e-mail marcado como pendente de verificação"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "success",
		"message":    fmt.Sprintf("Status de verificação atualizado: %s", statusMsg),
		"userId":     targetUserID,
		"isVerified": req.Verified,
	})
}

// GetSystemStats — GET /v1/admin/stats
func (h *AdminHandler) GetSystemStats(w http.ResponseWriter, r *http.Request) {
	users, err := h.userRepo.ListAllUsersForAdmin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao obter estatísticas")
		return
	}

	totalUsers := len(users)
	freeUsers := 0
	proUsers := 0

	for _, u := range users {
		if u.Plan == "pro" {
			proUsers++
		} else {
			freeUsers++
		}
	}

	totalJobs, _ := h.userRepo.CountTotalPlatformJobs(r.Context())

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "success",
		"totalUsers": totalUsers,
		"freeUsers":  freeUsers,
		"proUsers":   proUsers,
		"totalJobs":  totalJobs,
	})
}

// CheckCurrentAdminRole — GET /v1/admin/me
func (h *AdminHandler) CheckCurrentAdminRole(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Não autorizado")
		return
	}

	u, err := h.userRepo.FindUserByProjectID(r.Context(), proj.ID)
	if err != nil || u == nil {
		writeError(w, http.StatusNotFound, "Usuário não encontrado")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"isAdmin": u.Role == "admin",
		"role":    u.Role,
		"email":   u.Email,
	})
}

// DeleteUser — DELETE /v1/admin/users/{id}
func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Não autorizado")
		return
	}

	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		writeError(w, http.StatusBadRequest, "ID do usuário não especificado")
		return
	}
	targetUserID := pathParts[4]

	if targetUserID == proj.UserID {
		writeError(w, http.StatusBadRequest, "Você não pode excluir sua própria conta de Administrador")
		return
	}

	err := h.userRepo.DeleteUserByAdmin(r.Context(), targetUserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Erro ao excluir usuário: %v", err))
		return
	}

	if h.redis != nil {
		redisKey := fmt.Sprintf("ai_usage:%s", targetUserID)
		_ = h.redis.Del(r.Context(), redisKey).Err()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"message": "Conta de usuário excluída permanentemente com sucesso",
		"userId":  targetUserID,
	})
}
