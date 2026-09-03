package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/JanGustavo/Cron/internal/api/middleware"
	"github.com/JanGustavo/Cron/internal/config"
	userDomain "github.com/JanGustavo/Cron/internal/domain/user"
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
		} else if strings.HasPrefix(h.cfg.AsaasAPIKey, "$aact_prod_") || h.cfg.AppEnv == "production" {
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

// ========================================================================
// CEO DASHBOARD ENDPOINTS
// ========================================================================

// GetRevenueMetrics — GET /v1/admin/metrics/revenue
// Retorna métricas de receita (MRR, ARR, churn, LTV)
func (h *AdminHandler) GetRevenueMetrics(w http.ResponseWriter, r *http.Request) {
	users, err := h.userRepo.ListAllUsersForAdmin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao buscar usuários")
		return
	}

	var mrr, arr float64
	var activeSubs, canceledSubs int
	planCounts := make(map[string]int)

	for _, u := range users {
		if u.Plan == "pro" {
			if h.billingRepo != nil {
				sub, _ := h.billingRepo.GetSubscription(r.Context(), u.ID)
				if sub != nil {
					if sub.Status == "active" {
						activeSubs++
						if sub.BillingProvider == "asaas" && sub.ProviderSubscriptionID != nil {
							// Valor real do Asaas seria buscado aqui; usando valor fixo por plano
							mrr += 97.00 // Valor mensal PRO
							arr += 97.00 * 12
						} else {
							mrr += 97.00
							arr += 97.00 * 12
						}
					} else if sub.Status == "canceled" {
						canceledSubs++
					}
				} else {
					// Manual PRO
					mrr += 97.00
					arr += 97.00 * 12
					activeSubs++
				}
			}
		}
		planCounts[string(u.Plan)]++
	}

	churnRate := 0.0
	if activeSubs+canceledSubs > 0 {
		churnRate = float64(canceledSubs) / float64(activeSubs+canceledSubs) * 100
	}

	ltv := 0.0
	if churnRate > 0 {
		ltv = (mrr / churnRate) * 100
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":           "success",
		"mrr":              mrr,
		"arr":              arr,
		"activeSubs":       activeSubs,
		"canceledSubs":     canceledSubs,
		"churnRate":        churnRate,
		"ltv":              ltv,
		"planDistribution": planCounts,
	})
}

// GetUserGrowthMetrics — GET /v1/admin/metrics/users/growth
// Retorna crescimento de usuários por período
func (h *AdminHandler) GetUserGrowthMetrics(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "30d"
	}

	days := 30
	switch period {
	case "7d":
		days = 7
	case "30d":
		days = 30
	case "90d":
		days = 90
	case "365d":
		days = 365
	}

	users, err := h.userRepo.ListAllUsersForAdmin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao buscar usuários")
		return
	}

	cutoff := time.Now().AddDate(0, 0, -days)
	buckets := make(map[string]int)
	planBuckets := make(map[string]map[string]int)

	for _, u := range users {
		if u.CreatedAt.Before(cutoff) {
			continue
		}
		key := u.CreatedAt.Format("2006-01-02")
		buckets[key]++
		if planBuckets[key] == nil {
			planBuckets[key] = make(map[string]int)
		}
		planBuckets[key][string(u.Plan)]++
	}

	// Preencher dias sem usuários
	var growthData []map[string]any
	for d := 0; d < days; d++ {
		date := time.Now().AddDate(0, 0, -d)
		key := date.Format("2006-01-02")
		count := buckets[key]
		planData := planBuckets[key]
		if planData == nil {
			planData = map[string]int{"free": 0, "pro": 0}
		}
		growthData = append(growthData, map[string]any{
			"date":  key,
			"total": count,
			"free":  planData["free"],
			"pro":   planData["pro"],
		})
	}
	// Ordenar por data ascendente
	sort.Slice(growthData, func(i, j int) bool {
		return growthData[i]["date"].(string) < growthData[j]["date"].(string)
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "success",
		"period":   period,
		"data":     growthData,
		"totalNew": len(users), // Total no período
	})
}

// GetJobAnalytics — GET /v1/admin/metrics/jobs
// Retorna analytics de jobs (sucesso, falha, volume)
func (h *AdminHandler) GetJobAnalytics(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "7d"
	}

	days := 7
	switch period {
	case "1d":
		days = 1
	case "7d":
		days = 7
	case "30d":
		days = 30
	case "90d":
		days = 90
	}

	// Buscar execuções via SQL direto
	cutoff := time.Now().AddDate(0, 0, -days)

	query := `
		SELECT 
			DATE(e.triggered_at) as date,
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE e.status = 'success') as success,
			COUNT(*) FILTER (WHERE e.status = 'failed') as failed,
			COUNT(*) FILTER (WHERE e.status = 'timeout') as timeout,
			ROUND(AVG(e.duration_ms)) as avg_duration,
			MAX(e.duration_ms) as max_duration
		FROM executions e
		WHERE e.triggered_at >= $1
		GROUP BY DATE(e.triggered_at)
		ORDER BY date ASC
	`

	rows, err := h.userRepo.GetDB().QueryContext(r.Context(), query, cutoff)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao buscar analytics de jobs")
		return
	}
	defer rows.Close()

	var analytics []map[string]any
	for rows.Next() {
		var date time.Time
		var total, success, failed, timeout int
		var avgDur, maxDur sql.NullInt64
		if err := rows.Scan(&date, &total, &success, &failed, &timeout, &avgDur, &maxDur); err != nil {
			continue
		}
		successRate := 0.0
		if total > 0 {
			successRate = float64(success) / float64(total) * 100
		}
		analytics = append(analytics, map[string]any{
			"date":         date.Format("2006-01-02"),
			"total":        total,
			"success":      success,
			"failed":       failed,
			"timeout":      timeout,
			"success_rate": successRate,
			"avg_duration": avgDur.Int64,
			"max_duration": maxDur.Int64,
		})
	}

	// Totais do período
	var totalExecutions, totalSuccess, totalFailed int
	for _, a := range analytics {
		totalExecutions += a["total"].(int)
		totalSuccess += a["success"].(int)
		totalFailed += a["failed"].(int)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"period": period,
		"data":   analytics,
		"totals": map[string]any{
			"total_executions": totalExecutions,
			"total_success":    totalSuccess,
			"total_failed":     totalFailed,
			"success_rate":     float64(totalSuccess) / float64(max(1, totalExecutions)) * 100,
		},
	})
}

// GetSystemHealth — GET /v1/admin/metrics/system/health
// Retorna saúde do sistema (API, DB, Redis, Queue, AI)
func (h *AdminHandler) GetSystemHealth(w http.ResponseWriter, r *http.Request) {
	// DB health
	dbHealth := "healthy"
	dbLatency := int64(0)
	start := time.Now()
	err := h.userRepo.GetDB().PingContext(r.Context())
	dbLatency = time.Since(start).Milliseconds()
	if err != nil {
		dbHealth = "unhealthy"
	}

	// Redis health
	redisHealth := "healthy"
	redisLatency := int64(0)
	if h.redis != nil {
		start = time.Now()
		err = h.redis.Ping(r.Context()).Err()
		redisLatency = time.Since(start).Milliseconds()
		if err != nil {
			redisHealth = "unhealthy"
		}
	}

	// Queue metrics
	queueSize, activeJobs := int64(0), int64(0)
	if h.redis != nil {
		// Asynq queue inspection seria ideal aqui
		// Por enquanto valores mockados
		queueSize = 0
		activeJobs = 0
	}

	// AI status
	aiHealth := map[string]string{
		"gemini": "disabled",
		"groq":   "up",
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"database": map[string]any{
			"status":  dbHealth,
			"latency": dbLatency,
		},
		"redis": map[string]any{
			"status":  redisHealth,
			"latency": redisLatency,
		},
		"queue": map[string]any{
			"size":   queueSize,
			"active": activeJobs,
		},
		"ai":        aiHealth,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// GetAuditLogs — GET /v1/admin/audit-logs
// Retorna logs de auditoria de ações administrativas
func (h *AdminHandler) GetAuditLogs(w http.ResponseWriter, r *http.Request) {
	// Como não temos tabela de audit logs, retorna estrutura vazia
	// Em produção, seria uma tabela separada
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"data":    []map[string]any{},
		"message": "Audit logs não implementados ainda - usar logs de execução ou implementar tabela audit_logs",
	})
}

// ExportUsersCSV — GET /v1/admin/export/users
// Exporta lista de usuários em CSV
func (h *AdminHandler) ExportUsersCSV(w http.ResponseWriter, r *http.Request) {
	users, err := h.userRepo.ListAllUsersForAdmin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao buscar usuários")
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=users_export_%s.csv", time.Now().Format("20060102")))

	fmt.Fprintln(w, "ID,Email,Nome,Plano,Role,Verificado,Jobs,Ciclo,PeriodoFim,StatusAssinatura,ProvedorBilling,ClienteID,AssinaturaID,DataCriacao")

	for _, u := range users {
		jobCount, _ := h.userRepo.CountAllJobsByUserID(r.Context(), u.ID)

		var billingCycle, subStatus, billingProvider string
		var periodEnd string
		var providerCustID, providerSubID string

		if h.billingRepo != nil {
			sub, _ := h.billingRepo.GetSubscription(r.Context(), u.ID)
			if sub != nil {
				if sub.Status != "" {
					subStatus = sub.Status
				}
				if sub.BillingProvider != "" {
					billingProvider = sub.BillingProvider
				}
				if sub.CurrentPeriodEnd != nil {
					periodEnd = sub.CurrentPeriodEnd.Format("2006-01-02")
				}
				if sub.ProviderCustomerID != nil {
					providerCustID = *sub.ProviderCustomerID
				}
				if sub.ProviderSubscriptionID != nil {
					providerSubID = *sub.ProviderSubscriptionID
				}
				if sub.CurrentPeriodStart != nil && sub.CurrentPeriodEnd != nil {
					days := sub.CurrentPeriodEnd.Sub(*sub.CurrentPeriodStart).Hours() / 24
					if days > 60 {
						billingCycle = "yearly"
					} else {
						billingCycle = "monthly"
					}
				}
			}
		}

		if billingCycle == "" && u.Plan == "pro" {
			billingCycle = "monthly"
		}
		if subStatus == "" && u.Plan == "pro" {
			subStatus = "active"
		}
		if billingProvider == "" && u.Plan == "pro" {
			billingProvider = "manual"
		}

		_, _ = fmt.Fprint(w, buildUserCSVRow(u, jobCount, billingCycle, periodEnd, subStatus, billingProvider, providerCustID, providerSubID))
	}
}

func buildUserCSVRow(u *userDomain.User, jobCount int, billingCycle, periodEnd, subStatus, billingProvider, providerCustID, providerSubID string) string {
	return fmt.Sprintf("%s,%s,%s,%s,%s,%v,%d,%s,%s,%s,%s,%s,%s,%s\n",
		escapeCSV(u.ID),
		escapeCSV(u.Email),
		escapeCSV(u.FullName),
		escapeCSV(string(u.Plan)),
		escapeCSV(u.Role),
		u.IsVerified,
		jobCount,
		escapeCSV(billingCycle),
		escapeCSV(periodEnd),
		escapeCSV(subStatus),
		escapeCSV(billingProvider),
		escapeCSV(providerCustID),
		escapeCSV(providerSubID),
		escapeCSV(u.CreatedAt.Format("2006-01-02")),
	)
}

func escapeCSV(s string) string {
	if strings.Contains(s, ",") || strings.Contains(s, "\"") || strings.Contains(s, "\n") {
		s = strings.ReplaceAll(s, "\"", "\"\"")
		return "\"" + s + "\""
	}
	return s
}
