package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/JanGustavo/Cron/internal/api/middleware"
	"github.com/JanGustavo/Cron/internal/domain/execution"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/service"
)

// Mantém o import de execution ativo para o Swagger
var _ = execution.StatusSuccess

type ExecutionHandler struct {
	jobService    *service.JobService
	executionRepo *postgres.ExecutionRepository
}

const (
	defaultExecutionLimit = 50
	maxExecutionLimit     = 50000
)

func normalizeExecutionLimit(raw string, fallback, maxLimit int) int {
	if raw == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}

	if parsed > maxLimit {
		return maxLimit
	}

	return parsed
}

func NewExecutionHandler(jobService *service.JobService, executionRepo *postgres.ExecutionRepository) *ExecutionHandler {
	return &ExecutionHandler{jobService: jobService, executionRepo: executionRepo}
}

// List — GET /v1/jobs/{id}/executions?limit=50
// @Summary Listar execuções de um Job
// @Description Retorna a lista de tentativas de execução recentes de um Job específico.
// @Tags Executions
// @Produce json
// @Param id path string true "ID do Job"
// @Param limit query int false "Limite de resultados (1-200, padrão 50)"
// @Success 200 {array} execution.Execution "Lista de execuções"
// @Failure 401 {object} map[string]string "Não autenticado"
// @Failure 404 {object} map[string]string "Job não encontrado"
// @Failure 500 {object} map[string]string "Erro ao buscar execuções"
// @Security ApiKeyAuth
// @Router /v1/jobs/{id}/executions [get]
func (h *ExecutionHandler) List(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	jobID := chi.URLParam(r, "id")

	// Verifica que o job pertence ao projeto autenticado
	if _, err := h.jobService.GetByID(r.Context(), jobID, proj.ID); err != nil {
		writeError(w, http.StatusNotFound, "Job não encontrado")
		return
	}

	limit := normalizeExecutionLimit(r.URL.Query().Get("limit"), defaultExecutionLimit, maxExecutionLimit)

	executions, err := h.executionRepo.ListByJob(r.Context(), jobID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao buscar execuções")
		return
	}

	writeJSON(w, http.StatusOK, executions)
}

// ListGlobal — GET /v1/executions?page=1&limit=10
// @Summary Listar execuções globais do Projeto
// @Description Retorna a lista paginada de todas as execuções ocorridas nos Jobs do projeto autenticado.
// @Tags Executions
// @Produce json
// @Param page query int false "Número da página (padrão 1)"
// @Param limit query int false "Itens por página (1-100, padrão 10)"
// @Success 200 {object} map[string]any "Dados paginados contendo data ([]execution.ProjectExecution), total, page, limit"
// @Failure 401 {object} map[string]string "Não autenticado"
// @Failure 500 {object} map[string]string "Erro ao buscar execuções globais"
// @Security ApiKeyAuth
// @Router /v1/executions [get]
func (h *ExecutionHandler) ListGlobal(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())

	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	offset := (page - 1) * limit

	search := r.URL.Query().Get("search")
	status := r.URL.Query().Get("status")
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")

	executions, total, err := h.executionRepo.ListByProject(
		r.Context(),
		proj.ID,
		limit,
		offset,
		search,
		status,
		startDate,
		endDate,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao buscar execuções globais")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data":  executions,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}
