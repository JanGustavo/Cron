package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/JanGustavo/Cron/internal/domain/monitor"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
)

type MonitorService struct {
	repo         *postgres.MonitorRepository
	alertService *AlertService
	jwtSecret    string
}

func NewMonitorService(repo *postgres.MonitorRepository, alertService *AlertService, jwtSecret string) *MonitorService {
	return &MonitorService{
		repo:         repo,
		alertService: alertService,
		jwtSecret:    jwtSecret,
	}
}

// IngestAndEvaluate ingere o valor de uma chave e avalia contra as regras ativas cadastradas.
func (s *MonitorService) IngestAndEvaluate(ctx context.Context, projectID, key, value string) (*monitor.CheckResult, error) {
	if key == "" {
		return nil, fmt.Errorf("a chave não pode ser vazia")
	}

	// 1. Salva ou atualiza a chave no banco
	kv, err := s.repo.UpsertKeyValue(ctx, projectID, key, value)
	if err != nil {
		return nil, fmt.Errorf("MonitorService.IngestAndEvaluate: %w", err)
	}

	// 2. Busca todas as regras ativas para a chave neste projeto
	rules, err := s.repo.ListRulesByKey(ctx, projectID, key)
	if err != nil {
		return nil, fmt.Errorf("MonitorService.IngestAndEvaluate: %w", err)
	}

	result := &monitor.CheckResult{
		Key:          key,
		CurrentValue: kv.CurrentValue,
		Evaluated:    len(rules) > 0,
		Violated:     false,
		Rules:        make([]monitor.RuleStatus, 0, len(rules)),
	}

	// 3. Avalia cada regra
	for _, r := range rules {
		violated, msg := evaluateRule(kv.CurrentValue, r.Operator, r.ThresholdValue)
		status := monitor.RuleStatus{
			RuleID:   r.ID,
			RuleName: r.Name,
			Passed:   !violated,
			Message:  msg,
		}
		result.Rules = append(result.Rules, status)

		if violated {
			result.Violated = true

			// Dispara notificação via Webhook se configurado
			if r.WebhookURL != nil && *r.WebhookURL != "" {
				s.alertService.NotifyKeyViolation(
					*r.WebhookURL,
					projectID,
					s.jwtSecret,
					r.ID,
					r.Name,
					key,
					string(r.Operator),
					kv.CurrentValue,
					r.ThresholdValue,
				)
			}

			// Dispara e-mail se configurado
			if r.AlertEmail {
				s.alertService.NotifyEmailKeyViolation(
					projectID,
					r.Name,
					key,
					string(r.Operator),
					kv.CurrentValue,
					r.ThresholdValue,
				)
			}
		}
	}

	return result, nil
}

// GetKeyValue retorna o valor mais recente da chave informada.
func (s *MonitorService) GetKeyValue(ctx context.Context, projectID, key string) (*monitor.KeyValue, error) {
	if key == "" {
		return nil, fmt.Errorf("a chave não pode ser vazia")
	}
	return s.repo.GetKeyValue(ctx, projectID, key)
}

// CreateRule cria uma nova regra de monitoramento.
func (s *MonitorService) CreateRule(ctx context.Context, rule *monitor.MonitorRule) (*monitor.MonitorRule, error) {
	if rule.Name == "" || rule.Key == "" || rule.ThresholdValue == "" {
		return nil, fmt.Errorf("campos 'name', 'key' e 'threshold_value' são obrigatórios")
	}
	switch rule.Operator {
	case monitor.OpGt, monitor.OpGte, monitor.OpLt, monitor.OpLte, monitor.OpEq, monitor.OpNe, monitor.OpContains:
		// Válido
	default:
		return nil, fmt.Errorf("operador inválido: %s. Operadores suportados: gt, gte, lt, lte, eq, ne, contains", rule.Operator)
	}

	return s.repo.CreateRule(ctx, rule)
}

// ListRules retorna todas as regras do projeto.
func (s *MonitorService) ListRules(ctx context.Context, projectID string) ([]*monitor.MonitorRule, error) {
	return s.repo.ListRulesByProject(ctx, projectID)
}

// DeleteRule remove uma regra pelo ID.
func (s *MonitorService) DeleteRule(ctx context.Context, id, projectID string) error {
	return s.repo.DeleteRule(ctx, id, projectID)
}

// UpdateRule atualiza uma regra de monitoramento existente.
func (s *MonitorService) UpdateRule(ctx context.Context, rule *monitor.MonitorRule) (*monitor.MonitorRule, error) {
	if rule.Name == "" || rule.Key == "" || rule.ThresholdValue == "" {
		return nil, fmt.Errorf("campos 'name', 'key' e 'threshold_value' são obrigatórios")
	}
	switch rule.Operator {
	case monitor.OpGt, monitor.OpGte, monitor.OpLt, monitor.OpLte, monitor.OpEq, monitor.OpNe, monitor.OpContains:
		// Válido
	default:
		return nil, fmt.Errorf("operador inválido: %s. Operadores suportados: gt, gte, lt, lte, eq, ne, contains", rule.Operator)
	}

	return s.repo.UpdateRule(ctx, rule)
}

// EvaluateJobPayload avalia o corpo da resposta HTTP de uma execução de Job contra as regras de monitoramento cadastradas.
func (s *MonitorService) EvaluateJobPayload(ctx context.Context, projectID, jobID string, responseBody []byte) (bool, []monitor.RuleStatus, error) {
	if len(responseBody) == 0 {
		return false, nil, nil
	}

	// 1. Tenta parsear a resposta como JSON
	var payloadMap map[string]any
	if err := json.Unmarshal(responseBody, &payloadMap); err != nil {
		// Não é um JSON válido — pula avaliação sem gerar erro
		return false, nil, nil
	}

	// 2. Busca todas as regras associadas a este job específico
	rules, err := s.repo.ListRulesByJob(ctx, jobID)
	if err != nil {
		return false, nil, fmt.Errorf("MonitorService.EvaluateJobPayload: %w", err)
	}

	if len(rules) == 0 {
		return false, nil, nil
	}

	hasViolation := false
	statuses := make([]monitor.RuleStatus, 0, len(rules))

	// 3. Avalia cada regra cadastrada para o Job
	for _, r := range rules {
		curVal, found := extractJSONField(payloadMap, r.Key)
		if !found {
			// Campo não encontrado na resposta
			statuses = append(statuses, monitor.RuleStatus{
				RuleID:   r.ID,
				RuleName: r.Name,
				Passed:   false,
				Message:  fmt.Sprintf("Campo '%s' não encontrado no payload de resposta", r.Key),
			})
			continue
		}

		// Atualiza o valor conhecido da chave no banco
		_, _ = s.repo.UpsertKeyValue(ctx, projectID, r.Key, curVal)

		violated, msg := evaluateRule(curVal, r.Operator, r.ThresholdValue)
		status := monitor.RuleStatus{
			RuleID:   r.ID,
			RuleName: r.Name,
			Passed:   !violated,
			Message:  msg,
		}
		statuses = append(statuses, status)

		if violated {
			hasViolation = true

			if r.WebhookURL != nil && *r.WebhookURL != "" {
				s.alertService.NotifyKeyViolation(
					*r.WebhookURL,
					projectID,
					s.jwtSecret,
					r.ID,
					r.Name,
					r.Key,
					string(r.Operator),
					curVal,
					r.ThresholdValue,
				)
			}

			if r.AlertEmail {
				s.alertService.NotifyEmailKeyViolation(
					projectID,
					r.Name,
					r.Key,
					string(r.Operator),
					curVal,
					r.ThresholdValue,
				)
			}
		}
	}

	return hasViolation, statuses, nil
}

// extractJSONField extrai o valor de um campo JSON usando notação por pontos (ex: "data.accountsOnline" ou "goroutines_count").
func extractJSONField(data map[string]any, path string) (string, bool) {
	parts := strings.Split(path, ".")
	var current any = data

	for i, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		val, exists := m[part]
		if !exists {
			return "", false
		}
		if i == len(parts)-1 {
			if val == nil {
				return "", true
			}
			switch v := val.(type) {
			case string:
				return v, true
			case float64:
				if v == float64(int64(v)) {
					return fmt.Sprintf("%d", int64(v)), true
				}
				return fmt.Sprintf("%g", v), true
			case bool:
				return fmt.Sprintf("%t", v), true
			default:
				bytesVal, err := json.Marshal(v)
				if err != nil {
					return "", false
				}
				return string(bytesVal), true
			}
		}
		current = val
	}
	return "", false
}

// ListRulesByJob retorna todas as regras ativas de um job.
func (s *MonitorService) ListRulesByJob(ctx context.Context, jobID string) ([]*monitor.MonitorRule, error) {
	return s.repo.ListRulesByJob(ctx, jobID)
}

// evaluateRule verifica se o currentValue viola a regra definida por (operator, threshold).
// Retorna (true, msg) se houver VIOLAÇÃO da regra.
func evaluateRule(currentVal string, op monitor.Operator, threshold string) (bool, string) {
	// Tentamos conversão numérica para operadores matemáticos
	curNum, errCur := strconv.ParseFloat(currentVal, 64)
	threshNum, errThresh := strconv.ParseFloat(threshold, 64)
	isNumeric := (errCur == nil && errThresh == nil)

	switch op {
	case monitor.OpGt: // Esperado > threshold. Violado se <=
		if isNumeric {
			if curNum <= threshNum {
				return true, fmt.Sprintf("Valor atual (%.2f) não é maior que o limite (%.2f)", curNum, threshNum)
			}
		} else {
			if currentVal <= threshold {
				return true, fmt.Sprintf("Valor '%s' não é maior que '%s'", currentVal, threshold)
			}
		}
	case monitor.OpGte: // Esperado >= threshold. Violado se <
		if isNumeric {
			if curNum < threshNum {
				return true, fmt.Sprintf("Valor atual (%.2f) é menor que o limite mínimo (%.2f)", curNum, threshNum)
			}
		} else {
			if currentVal < threshold {
				return true, fmt.Sprintf("Valor '%s' é menor que '%s'", currentVal, threshold)
			}
		}
	case monitor.OpLt: // Esperado < threshold. Violado se >=
		if isNumeric {
			if curNum >= threshNum {
				return true, fmt.Sprintf("Valor atual (%.2f) atingiu ou excedeu o limite máximo (%.2f)", curNum, threshNum)
			}
		} else {
			if currentVal >= threshold {
				return true, fmt.Sprintf("Valor '%s' é maior ou igual a '%s'", currentVal, threshold)
			}
		}
	case monitor.OpLte: // Esperado <= threshold. Violado se >
		if isNumeric {
			if curNum > threshNum {
				return true, fmt.Sprintf("Valor atual (%.2f) excedeu o limite máximo (%.2f)", curNum, threshNum)
			}
		} else {
			if currentVal > threshold {
				return true, fmt.Sprintf("Valor '%s' é maior que '%s'", currentVal, threshold)
			}
		}
	case monitor.OpEq: // Esperado == threshold. Violado se !=
		if currentVal != threshold {
			return true, fmt.Sprintf("Valor atual '%s' é diferente do esperado '%s'", currentVal, threshold)
		}
	case monitor.OpNe: // Esperado != threshold. Violado se ==
		if currentVal == threshold {
			return true, fmt.Sprintf("Valor atual '%s' é igual ao valor proibido '%s'", currentVal, threshold)
		}
	case monitor.OpContains: // Esperado conter substring. Violado se não conter
		if !strings.Contains(currentVal, threshold) {
			return true, fmt.Sprintf("Valor atual '%s' não contém '%s'", currentVal, threshold)
		}
	}

	return false, ""
}

