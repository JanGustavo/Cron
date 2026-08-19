package handler

import (
	"encoding/json"
	"errors"
	"log"
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
// @Summary Criar Job
// @Description Cria um novo agendamento de Job para o projeto autenticado.
// @Tags Jobs
// @Accept json
// @Produce json
// @Param job body object true "Dados para criação do Job (name, schedule, url, etc.)"
// @Success 201 {object} job.Job "Job criado com sucesso"
// @Failure 400 {object} map[string]string "Parâmetros inválidos ou corpo incorreto"
// @Failure 401 {object} map[string]string "API Key não fornecida ou inválida"
// @Failure 403 {object} map[string]string "Limite de jobs do plano atingido"
// @Failure 500 {object} map[string]string "Erro interno do servidor"
// @Security ApiKeyAuth
// @Router /v1/jobs [post]
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
		NextJobID       *string           `json:"next_job_id"`
		Tags            []string          `json:"tags"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Body invalido")
		return
	}

	// Validacoes basicas de campo obrigatorio
	if input.Name == "" || input.Schedule == "" || input.URL == "" {
		writeError(w, http.StatusBadRequest, "Name, schedule e url sao obrigatorios")
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
		NextJobID:       input.NextJobID,
		Tags:            input.Tags,
	})
	if err != nil {
		log.Printf("ERROR JobHandler.Create: %v", err)
		switch {
		case errors.Is(err, service.ErrInvalidSchedule):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrJobLimitReached):
			writeError(w, http.StatusForbidden, "Limite de jobs do plano atingido - faca upgrade")
		case errors.Is(err, service.ErrWebhookAlertsDisabled):
			writeError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, service.ErrWorkflowsDisabled):
			writeError(w, http.StatusForbidden, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "Erro ao criar job")
		}
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

// List — GET /v1/jobs
// @Summary Listar Jobs
// @Description Retorna todos os agendamentos de Jobs do projeto autenticado.
// @Tags Jobs
// @Produce json
// @Success 200 {array} job.Job "Lista de jobs do projeto"
// @Failure 401 {object} map[string]string "Não autenticado"
// @Failure 500 {object} map[string]string "Erro ao listar jobs"
// @Security ApiKeyAuth
// @Router /v1/jobs [get]
func (h *JobHandler) List(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())

	jobs, err := h.jobService.List(r.Context(), proj.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao listar jobs")
		return
	}

	writeJSON(w, http.StatusOK, jobs)
}

// GetByID — GET /v1/jobs/{id}
// @Summary Buscar Job por ID
// @Description Retorna os detalhes de um Job específico pelo ID, desde que pertença ao projeto autenticado.
// @Tags Jobs
// @Produce json
// @Param id path string true "ID do Job"
// @Success 200 {object} job.Job "Detalhes do job"
// @Failure 401 {object} map[string]string "Não autenticado"
// @Failure 403 {object} map[string]string "Acesso negado ao job de outro projeto"
// @Failure 404 {object} map[string]string "Job não encontrado"
// @Failure 500 {object} map[string]string "Erro ao buscar job"
// @Security ApiKeyAuth
// @Router /v1/jobs/{id} [get]
func (h *JobHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	id := chi.URLParam(r, "id")

	j, err := h.jobService.GetByID(r.Context(), id, proj.ID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrJobNotFound):
			writeError(w, http.StatusNotFound, "Job nao encontrado")
		case errors.Is(err, service.ErrUnauthorized):
			writeError(w, http.StatusForbidden, "Acesso negado")
		default:
			writeError(w, http.StatusInternalServerError, "Erro ao buscar job")
		}
		return
	}

	writeJSON(w, http.StatusOK, j)
}

// UpdateStatus — PATCH /v1/jobs/{id}
// @Summary Atualizar Status do Job
// @Description Altera o status do Job entre 'active' e 'paused'.
// @Tags Jobs
// @Accept json
// @Param id path string true "ID do Job"
// @Param body body object true "Novo status (active ou paused)"
// @Success 204 "Status atualizado com sucesso"
// @Failure 400 {object} map[string]string "Status inválido ou corpo incorreto"
// @Failure 401 {object} map[string]string "Não autenticado"
// @Failure 404 {object} map[string]string "Job não encontrado"
// @Failure 500 {object} map[string]string "Erro ao atualizar status"
// @Security ApiKeyAuth
// @Router /v1/jobs/{id} [patch]
func (h *JobHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	id := chi.URLParam(r, "id")

	var input struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Body invalido")
		return
	}

	status := job.Status(input.Status)
	if status != job.StatusActive && status != job.StatusPaused {
		writeError(w, http.StatusBadRequest, "Status deve ser 'active' ou 'paused'")
		return
	}

	if err := h.jobService.UpdateStatus(r.Context(), id, proj.ID, status); err != nil {
		switch {
		case errors.Is(err, service.ErrJobNotFound):
			writeError(w, http.StatusNotFound, "Job nao encontrado")
		default:
			writeError(w, http.StatusInternalServerError, "Erro ao atualizar status")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Delete — DELETE /v1/jobs/{id}
// @Summary Excluir Job
// @Description Remove um Job do banco de dados e limpa os agendamentos associados.
// @Tags Jobs
// @Param id path string true "ID do Job"
// @Success 204 "Job deletado com sucesso"
// @Failure 401 {object} map[string]string "Não autenticado"
// @Failure 404 {object} map[string]string "Job não encontrado"
// @Failure 500 {object} map[string]string "Erro ao deletar job"
// @Security ApiKeyAuth
// @Router /v1/jobs/{id} [delete]
func (h *JobHandler) Delete(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.jobService.Delete(r.Context(), id, proj.ID); err != nil {
		switch {
		case errors.Is(err, service.ErrJobNotFound):
			writeError(w, http.StatusNotFound, "Job nao encontrado")
		default:
			writeError(w, http.StatusInternalServerError, "Erro ao deletar job")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// TriggerNow — POST /v1/jobs/{id}/trigger
// @Summary Disparar Job Imediatamente
// @Description Enfileira a execução de um Job de forma manual e imediata, enviando para o processamento em background.
// @Tags Jobs
// @Produce json
// @Param id path string true "ID do Job"
// @Success 200 {object} map[string]string "Execução manual enfileirada com sucesso"
// @Failure 401 {object} map[string]string "Não autenticado"
// @Failure 403 {object} map[string]string "Acesso negado ao job de outro projeto"
// @Failure 404 {object} map[string]string "Job não encontrado"
// @Failure 500 {object} map[string]string "Erro interno ao enfileirar execução"
// @Security ApiKeyAuth
// @Router /v1/jobs/{id}/trigger [post]
func (h *JobHandler) TriggerNow(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	jobID := chi.URLParam(r, "id")

	err := h.jobService.TriggerNow(r.Context(), jobID, proj.ID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrJobNotFound):
			writeError(w, http.StatusNotFound, "Job nao encontrado")
		case errors.Is(err, service.ErrUnauthorized):
			writeError(w, http.StatusForbidden, "Acesso negado")
		default:
			writeError(w, http.StatusInternalServerError, "Erro ao disparar tarefa")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "tarefa enfileirada para execucao imediata",
	})
}

// Update — PUT /v1/jobs/{id}
// @Summary Atualizar Configurações do Job
// @Description Altera os campos configuráveis de um Job (nome, cron, url, etc.).
// @Tags Jobs
// @Accept json
// @Produce json
// @Param id path string true "ID do Job"
// @Param body body object true "Campos do Job a serem atualizados"
// @Success 200 {object} job.Job "Job atualizado com sucesso"
// @Failure 400 {object} map[string]string "Parâmetros inválidos"
// @Failure 401 {object} map[string]string "Não autenticado"
// @Failure 404 {object} map[string]string "Job não encontrado"
// @Failure 500 {object} map[string]string "Erro ao atualizar job"
// @Security ApiKeyAuth
// @Router /v1/jobs/{id} [put]
func (h *JobHandler) Update(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	id := chi.URLParam(r, "id")

	var input struct {
		Name            string            `json:"name"`
		Schedule        string            `json:"schedule"`
		Timezone        string            `json:"timezone"`
		URL             string            `json:"url"`
		HTTPMethod      string            `json:"http_method"`
		Headers         map[string]string `json:"headers"`
		Payload         map[string]any    `json:"payload"`
		WebhookAlertURL *string           `json:"webhook_alert_url"`
		NextJobID       *string           `json:"next_job_id"`
		Tags            []string          `json:"tags"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Body invalido")
		return
	}

	if input.Name == "" || input.Schedule == "" || input.URL == "" {
		writeError(w, http.StatusBadRequest, "Name, schedule e url sao obrigatorios")
		return
	}

	updated, err := h.jobService.Update(r.Context(), service.UpdateJobInput{
		ID:              id,
		ProjectID:       proj.ID,
		Name:            input.Name,
		Schedule:        input.Schedule,
		Timezone:        input.Timezone,
		URL:             input.URL,
		HTTPMethod:      input.HTTPMethod,
		Headers:         input.Headers,
		Payload:         input.Payload,
		WebhookAlertURL: input.WebhookAlertURL,
		NextJobID:       input.NextJobID,
		Tags:            input.Tags,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrJobNotFound):
			writeError(w, http.StatusNotFound, "Job nao encontrado")
		case errors.Is(err, service.ErrInvalidSchedule):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrWebhookAlertsDisabled):
			writeError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, service.ErrWorkflowsDisabled):
			writeError(w, http.StatusForbidden, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "Erro ao atualizar job")
		}
		return
	}

	writeJSON(w, http.StatusOK, updated)
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
