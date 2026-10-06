package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net/http"
	"strings"

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
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512*1024))
	decoder.UseNumber()
	if err := decoder.Decode(&input); err != nil {
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
		Mode           string  `json:"mode"`
		ValueType      string  `json:"value_type"`
		JobID          *string `json:"job_id"`
		Name           string  `json:"name"`
		Key            string  `json:"key"`
		Operator       string  `json:"operator"`
		ThresholdValue any     `json:"threshold_value"`
		AlertEmail     *bool   `json:"alert_email"`
		WebhookURL     *string `json:"webhook_url"`
		IsEnabled      *bool   `json:"is_enabled"`
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512*1024))
	decoder.UseNumber()
	if err := decoder.Decode(&input); err != nil {
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

	var jobIDPtr *string
	if input.JobID != nil && *input.JobID != "" && *input.JobID != "all" && *input.JobID != "global" {
		jobIDPtr = input.JobID
	}

	rule := &monitor.MonitorRule{
		Mode: input.Mode, ValueType: input.ValueType,
		ProjectID:      proj.ID,
		JobID:          jobIDPtr,
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
		Mode           string  `json:"mode"`
		ValueType      string  `json:"value_type"`
		Name           string  `json:"name"`
		Key            string  `json:"key"`
		Operator       string  `json:"operator"`
		ThresholdValue any     `json:"threshold_value"`
		AlertEmail     *bool   `json:"alert_email"`
		WebhookURL     *string `json:"webhook_url"`
		IsEnabled      *bool   `json:"is_enabled"`
		JobID          *string `json:"job_id"` // Opcional: vincular a um job
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512*1024))
	decoder.UseNumber()
	if err := decoder.Decode(&input); err != nil {
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
		Mode: input.Mode, ValueType: input.ValueType,
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

// PreviewKey evaluates a typed sample without updating metrics or dispatching alerts.
func (h *MonitorHandler) PreviewKey(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Projeto não autenticado")
		return
	}
	var input struct {
		Key   string          `json:"key"`
		JobID string          `json:"job_id"`
		Value json.RawMessage `json:"value"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024))
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Corpo de requisição inválido ou muito grande")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		writeError(w, http.StatusBadRequest, "Envie somente um documento JSON")
		return
	}
	if strings.TrimSpace(input.Key) == "" || len(input.Key) > 100 || len(input.Value) == 0 {
		writeError(w, http.StatusBadRequest, "Informe a chave e o valor da amostra")
		return
	}
	if input.JobID != "" {
		if _, err := uuid.Parse(input.JobID); err != nil {
			writeError(w, http.StatusBadRequest, "Job inválido")
			return
		}
	}
	valueDecoder := json.NewDecoder(bytes.NewReader(input.Value))
	valueDecoder.UseNumber()
	var value any
	if err := valueDecoder.Decode(&value); err != nil {
		writeError(w, http.StatusBadRequest, "Amostra inválida")
		return
	}
	result, err := h.service.PreviewKey(r.Context(), proj.ID, input.JobID, input.Key, value)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Não foi possível avaliar a amostra neste projeto")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *MonitorHandler) PreviewRule(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Projeto não autenticado")
		return
	}
	var input struct {
		Rule         monitor.MonitorRule `json:"rule"`
		ResponseBody string              `json:"response_body"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Amostra inválida ou muito grande")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		writeError(w, http.StatusBadRequest, "Envie somente um documento JSON")
		return
	}
	if input.Rule.JobID == nil {
		writeError(w, http.StatusBadRequest, "Selecione um job para testar a condição")
		return
	}
	if _, err := uuid.Parse(*input.Rule.JobID); err != nil {
		writeError(w, http.StatusBadRequest, "Job inválido")
		return
	}
	input.Rule.ProjectID = proj.ID
	statuses, err := h.service.PreviewRule(r.Context(), &input.Rule, []byte(input.ResponseBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"evaluations": statuses, "passed": monitor.SuccessConditionsPassed(statuses) && !monitor.HasViolation(statuses)})
}
