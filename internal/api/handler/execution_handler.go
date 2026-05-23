package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/JanGustavo/Cron/internal/api/middleware"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/service"
)

type ExecutionHandler struct {
	jobService    *service.JobService
	executionRepo *postgres.ExecutionRepository
}

func NewExecutionHandler(jobService *service.JobService, executionRepo *postgres.ExecutionRepository) *ExecutionHandler {
	return &ExecutionHandler{jobService: jobService, executionRepo: executionRepo}
}

// List — GET /v1/jobs/{id}/executions?limit=50
func (h *ExecutionHandler) List(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	jobID := chi.URLParam(r, "id")

	// Verifica que o job pertence ao projeto autenticado
	if _, err := h.jobService.GetByID(r.Context(), jobID, proj.ID); err != nil {
		writeError(w, http.StatusNotFound, "job não encontrado")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}

	executions, err := h.executionRepo.ListByJob(r.Context(), jobID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao buscar execuções")
		return
	}

	writeJSON(w, http.StatusOK, executions)
}
