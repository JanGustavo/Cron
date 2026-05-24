package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/JanGustavo/Cron/internal/api/middleware"
	"github.com/JanGustavo/Cron/internal/domain/job"
	"github.com/JanGustavo/Cron/internal/service"
)

type JobHandler struct {
	jobService *service.JobService
}

func NewJobHandler(jobService *service.JobService) *JobHandler {
	return &JobHandler{jobService: jobService}
}

// Create — POST /v1/jobs
func (h *JobHandler) Create(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())

	var input struct {
		Name            string            `json:"name"`
		Schedule        string            `json:"schedule"`
		Timezone        string            `json:"timezone"`
		URL             string            `json:"url"`
		HTTPMethod      string            `json:"http_method"`
		Headers         map[string]string `json:"headers"`
		Payload         map[string]any    `json:"payload"`
		WebhookAlertURL *string           `json:"webhook_alert_url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "body invalido")
		return
	}

	// Validacoes basicas de campo obrigatorio
	if input.Name == "" || input.Schedule == "" || input.URL == "" {
		writeError(w, http.StatusBadRequest, "name, schedule e url sao obrigatorios")
		return
	}

	method := job.HTTPMethod(input.HTTPMethod)
	if method == "" {
		method = job.MethodPost
	}

	created, err := h.jobService.Create(r.Context(), service.CreateJobInput{
		ProjectID:       proj.ID,
		Name:            input.Name,
		Schedule:        input.Schedule,
		Timezone:        input.Timezone,
		URL:             input.URL,
		HTTPMethod:      method,
		Headers:         input.Headers,
		Payload:         input.Payload,
		WebhookAlertURL: input.WebhookAlertURL,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidSchedule):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrJobLimitReached):
			writeError(w, http.StatusForbidden, "limite de jobs do plano atingido - faca upgrade")
		default:
			writeError(w, http.StatusInternalServerError, "erro ao criar job")
		}
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

// List — GET /v1/jobs
func (h *JobHandler) List(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())

	jobs, err := h.jobService.List(r.Context(), proj.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao listar jobs")
		return
	}

	writeJSON(w, http.StatusOK, jobs)
}

// GetByID — GET /v1/jobs/{id}
func (h *JobHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	id := chi.URLParam(r, "id")

	j, err := h.jobService.GetByID(r.Context(), id, proj.ID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrJobNotFound):
			writeError(w, http.StatusNotFound, "job nao encontrado")
		case errors.Is(err, service.ErrUnauthorized):
			writeError(w, http.StatusForbidden, "acesso negado")
		default:
			writeError(w, http.StatusInternalServerError, "erro ao buscar job")
		}
		return
	}

	writeJSON(w, http.StatusOK, j)
}

// UpdateStatus — PATCH /v1/jobs/{id}
func (h *JobHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	id := chi.URLParam(r, "id")

	var input struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "body invalido")
		return
	}

	status := job.Status(input.Status)
	if status != job.StatusActive && status != job.StatusPaused {
		writeError(w, http.StatusBadRequest, "status deve ser 'active' ou 'paused'")
		return
	}

	if err := h.jobService.UpdateStatus(r.Context(), id, proj.ID, status); err != nil {
		switch {
		case errors.Is(err, service.ErrJobNotFound):
			writeError(w, http.StatusNotFound, "job nao encontrado")
		default:
			writeError(w, http.StatusInternalServerError, "erro ao atualizar status")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Delete — DELETE /v1/jobs/{id}
func (h *JobHandler) Delete(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.jobService.Delete(r.Context(), id, proj.ID); err != nil {
		switch {
		case errors.Is(err, service.ErrJobNotFound):
			writeError(w, http.StatusNotFound, "job nao encontrado")
		default:
			writeError(w, http.StatusInternalServerError, "erro ao deletar job")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// TriggerNow — POST /v1/jobs/{id}/trigger
func (h *JobHandler) TriggerNow(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	jobID := chi.URLParam(r, "id")

	err := h.jobService.TriggerNow(r.Context(), jobID, proj.ID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrJobNotFound):
			writeError(w, http.StatusNotFound, "job nao encontrado")
		case errors.Is(err, service.ErrUnauthorized):
			writeError(w, http.StatusForbidden, "acesso negado")
		default:
			writeError(w, http.StatusInternalServerError, "erro ao disparar tarefa")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "tarefa enfileirada para execucao imediata",
	})
}

// writeJSON serializa qualquer valor para JSON e escreve na response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError escreve uma resposta de erro padronizada.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
