package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/JanGustavo/Cron/internal/api/middleware"
	"github.com/JanGustavo/Cron/internal/domain/monitor"
	"github.com/JanGustavo/Cron/internal/service"
)

type MonitorHandler struct {
	service *service.MonitorService
}

func NewMonitorHandler(service *service.MonitorService) *MonitorHandler {
	return &MonitorHandler{service: service}
}

// CheckPayload — POST /v1/monitor/check
// @Summary Ingerir e Verificar Payload
// @Description Atualiza o valor de uma chave e avalia contra as regras de negócio cadastradas.
// @Tags Monitor
// @Accept json
// @Produce json
// @Param body body object true "Payload com chave e valor"
// @Success 200 {object} monitor.CheckResult "Resultado da avaliação das regras"
// @Failure 400 {object} map[string]string "Parâmetros inválidos"
// @Failure 401 {object} map[string]string "Não autenticado"
// @Failure 500 {object} map[string]string "Erro interno"
// @Security ApiKeyAuth
// @Router /v1/monitor/check [post]
func (h *MonitorHandler) CheckPayload(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Projeto não autenticado")
		return
	}

	var input struct {
		Key   string `json:"key"`
		Value any    `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Corpo de requisição inválido")
		return
	}
	if input.Key == "" {
		writeError(w, http.StatusBadRequest, "O campo 'key' é obrigatório")
		return
	}
	if input.Value == nil {
		writeError(w, http.StatusBadRequest, "O campo 'value' é obrigatório")
		return
	}

	valStr, err := formatValueAsString(input.Value)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Valor inválido para o campo 'value'")
		return
	}

	res, err := h.service.IngestAndEvaluate(r.Context(), proj.ID, input.Key, valStr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, res)
}

// GetKey — GET /v1/monitor/keys/{key}
// @Summary Consultar Valor de uma Chave
// @Description Retorna o último valor registrado de uma chave de monitoramento do projeto.
// @Tags Monitor
// @Produce json
// @Param key path string true "Nome da chave"
// @Success 200 {object} monitor.KeyValue "Valor atual da chave"
// @Failure 404 {object} map[string]string "Chave não encontrada"
// @Failure 401 {object} map[string]string "Não autenticado"
// @Router /v1/monitor/keys/{key} [get]
func (h *MonitorHandler) GetKey(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Projeto não autenticado")
		return
	}

	key := chi.URLParam(r, "key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "Parâmetro 'key' é obrigatório na URL")
		return
	}

	kv, err := h.service.GetKeyValue(r.Context(), proj.ID, key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao buscar chave")
		return
	}
	if kv == nil {
		writeError(w, http.StatusNotFound, "Chave não encontrada")
		return
	}

	writeJSON(w, http.StatusOK, kv)
}

// CreateRule — POST /v1/monitor/rules
// @Summary Criar Regra de Monitoramento
// @Description Cadastra uma nova regra de negócio associada a uma chave de monitoramento.
// @Tags Monitor
// @Accept json
// @Produce json
// @Param body body object true "Dados da Regra"
// @Success 201 {object} monitor.MonitorRule "Regra criada com sucesso"
// @Failure 400 {object} map[string]string "Parâmetros inválidos"
// @Router /v1/monitor/rules [post]
func (h *MonitorHandler) CreateRule(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Projeto não autenticado")
		return
	}

	var input struct {
		Name           string  `json:"name"`
		Key            string  `json:"key"`
		Operator       string  `json:"operator"`
		ThresholdValue any     `json:"threshold_value"`
		AlertEmail     *bool   `json:"alert_email"`
		WebhookURL     *string `json:"webhook_url"`
		IsEnabled      *bool   `json:"is_enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Corpo de requisição inválido")
		return
	}

	threshStr, err := formatValueAsString(input.ThresholdValue)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Campo 'threshold_value' inválido")
		return
	}

	alertEmail := true
	if input.AlertEmail != nil {
		alertEmail = *input.AlertEmail
	}

	isEnabled := true
	if input.IsEnabled != nil {
		isEnabled = *input.IsEnabled
	}

	rule := &monitor.MonitorRule{
		ProjectID:      proj.ID,
		Name:           input.Name,
		Key:            input.Key,
		Operator:       monitor.Operator(input.Operator),
		ThresholdValue: threshStr,
		AlertEmail:     alertEmail,
		WebhookURL:     input.WebhookURL,
		IsEnabled:      isEnabled,
	}

	created, err := h.service.CreateRule(r.Context(), rule)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

// ListRules — GET /v1/monitor/rules
// @Summary Listar Regras de Monitoramento
// @Description Retorna todas as regras cadastradas para o projeto autenticado.
// @Tags Monitor
// @Produce json
// @Success 200 {array} monitor.MonitorRule "Lista de regras"
// @Router /v1/monitor/rules [get]
func (h *MonitorHandler) ListRules(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Projeto não autenticado")
		return
	}

	rules, err := h.service.ListRules(r.Context(), proj.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao listar regras")
		return
	}

	writeJSON(w, http.StatusOK, rules)
}

// DeleteRule — DELETE /v1/monitor/rules/{id}
// @Summary Deletar Regra de Monitoramento
// @Description Remove uma regra pelo ID.
// @Tags Monitor
// @Param id path string true "ID da regra"
// @Success 204 "Regra deletada com sucesso"
// @Router /v1/monitor/rules/{id} [delete]
func (h *MonitorHandler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Projeto não autenticado")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "ID da regra é obrigatório")
		return
	}

	if err := h.service.DeleteRule(r.Context(), id, proj.ID); err != nil {
		writeError(w, http.StatusNotFound, "Regra não encontrada ou erro ao deletar")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// UpdateRule — PUT /v1/monitor/rules/{id}
// @Summary Atualizar Regra de Monitoramento
// @Description Atualiza uma regra de monitoramento existente.
// @Tags Monitor
// @Accept json
// @Produce json
// @Param id path string true "ID da regra"
// @Param body body object true "Dados da Regra"
// @Success 200 {object} monitor.MonitorRule "Regra atualizada com sucesso"
// @Failure 400 {object} map[string]string "Parâmetros inválidos"
// @Failure 404 {object} map[string]string "Regra não encontrada"
// @Router /v1/monitor/rules/{id} [put]
func (h *MonitorHandler) UpdateRule(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Projeto não autenticado")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "ID da regra é obrigatório")
		return
	}

	var input struct {
		Name           string  `json:"name"`
		Key            string  `json:"key"`
		Operator       string  `json:"operator"`
		ThresholdValue any     `json:"threshold_value"`
		AlertEmail     *bool   `json:"alert_email"`
		WebhookURL     *string `json:"webhook_url"`
		IsEnabled      *bool   `json:"is_enabled"`
		JobID          *string `json:"job_id"` // Opcional: vincular a um job
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Corpo de requisição inválido")
		return
	}

	threshStr, err := formatValueAsString(input.ThresholdValue)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Campo 'threshold_value' inválido")
		return
	}

	alertEmail := true
	if input.AlertEmail != nil {
		alertEmail = *input.AlertEmail
	}

	isEnabled := true
	if input.IsEnabled != nil {
		isEnabled = *input.IsEnabled
	}

	rule := &monitor.MonitorRule{
		ID:             id,
		ProjectID:      proj.ID,
		JobID:          input.JobID,
		Name:           input.Name,
		Key:            input.Key,
		Operator:       monitor.Operator(input.Operator),
		ThresholdValue: threshStr,
		AlertEmail:     alertEmail,
		WebhookURL:     input.WebhookURL,
		IsEnabled:      isEnabled,
	}

	updated, err := h.service.UpdateRule(r.Context(), rule)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func formatValueAsString(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	switch val := v.(type) {
	case string:
		return val, nil
	case float64:
		// Trata inteiros sem casas decimais (ex: 8 -> "8") e floats reais (ex: 12.5 -> "12.5")
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val)), nil
		}
		return fmt.Sprintf("%g", val), nil
	case int:
		return fmt.Sprintf("%d", val), nil
	case int64:
		return fmt.Sprintf("%d", val), nil
	case json.Number:
		return val.String(), nil
	default:
		data, err := json.Marshal(val)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
}
