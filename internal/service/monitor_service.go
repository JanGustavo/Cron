package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/JanGustavo/Cron/internal/domain/monitor"
)

type monitorRepository interface {
	UpsertKeyValue(context.Context, string, string, string) (*monitor.KeyValue, error)
	GetKeyValue(context.Context, string, string) (*monitor.KeyValue, error)
	ListRulesByKey(context.Context, string, string) ([]*monitor.MonitorRule, error)
	ListRulesByJob(context.Context, string, string) ([]*monitor.MonitorRule, error)
	ListRulesByProject(context.Context, string) ([]*monitor.MonitorRule, error)
	CreateRule(context.Context, *monitor.MonitorRule) (*monitor.MonitorRule, error)
	UpdateRule(context.Context, *monitor.MonitorRule) (*monitor.MonitorRule, error)
	DeleteRule(context.Context, string, string) error
	JobBelongsToProject(context.Context, string, string) (bool, error)
}

type monitorAlerts interface {
	NotifyKeyViolation(string, string, string, string, string, string, string, string, string)
	NotifyEmailKeyViolation(string, string, string, string, string, string)
}

type MonitorService struct {
	repo         monitorRepository
	alertService monitorAlerts
	jwtSecret    string
}

func NewMonitorService(repo monitorRepository, alertService monitorAlerts, jwtSecret string) *MonitorService {
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
			s.notifyViolation(projectID, r, kv.CurrentValue)
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
	if err := s.validateRule(ctx, rule); err != nil {
		return nil, err
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
	// Older clients omit new fields; never silently remove a success gate.
	if rule.Mode == "" || rule.ValueType == "" {
		existing, err := s.repo.ListRulesByProject(ctx, rule.ProjectID)
		if err != nil {
			return nil, err
		}
		for _, saved := range existing {
			if saved.ID == rule.ID {
				if rule.Mode == "" {
					rule.Mode = saved.Mode
				}
				if rule.ValueType == "" {
					rule.ValueType = saved.ValueType
				}
				break
			}
		}
	}
	if err := s.validateRule(ctx, rule); err != nil {
		return nil, err
	}
	return s.repo.UpdateRule(ctx, rule)
}

// EvaluateJobPayload remains compatible with callers that already have a full response.
func (s *MonitorService) EvaluateJobPayload(ctx context.Context, projectID, jobID string, body []byte) (bool, []monitor.RuleStatus, error) {
	return s.EvaluateJobResponse(ctx, projectID, jobID, monitor.Response{Body: body, Received: true})
}

func (s *MonitorService) EvaluateJobResponse(ctx context.Context, projectID, jobID string, response monitor.Response) (bool, []monitor.RuleStatus, error) {
	rules, err := s.repo.ListRulesByJob(ctx, projectID, jobID)
	if err != nil {
		return false, nil, fmt.Errorf("MonitorService.EvaluateJobResponse: %w", err)
	}
	statuses := monitor.EvaluateResponse(rules, response)
	for i, rule := range rules {
		if statuses[i].Status == "fail" {
			value := statuses[i].Message
			if len(statuses[i].ObservedValue) > 0 {
				value = string(statuses[i].ObservedValue)
				var text string
				if json.Unmarshal(statuses[i].ObservedValue, &text) == nil {
					value = text
				}
			}
			s.notifyViolation(projectID, rule, value)
		}
	}
	return monitor.HasViolation(statuses), statuses, nil
}

func (s *MonitorService) notifyViolation(projectID string, rule *monitor.MonitorRule, value string) {
	if s.alertService == nil {
		return
	}
	if rule.WebhookURL != nil && *rule.WebhookURL != "" {
		s.alertService.NotifyKeyViolation(*rule.WebhookURL, projectID, s.jwtSecret, rule.ID, rule.Name, rule.Key, string(rule.Operator), value, rule.ThresholdValue)
	}
	if rule.AlertEmail {
		s.alertService.NotifyEmailKeyViolation(projectID, rule.Name, rule.Key, string(rule.Operator), value, rule.ThresholdValue)
	}
}

// PreviewKey evaluates configured rules without storing a sample or sending alerts.
func (s *MonitorService) PreviewKey(ctx context.Context, projectID, jobID, key string, value any) (*monitor.CheckResult, error) {
	var rules []*monitor.MonitorRule
	var err error
	if jobID == "" {
		rules, err = s.repo.ListRulesByKey(ctx, projectID, key)
	} else {
		if err = s.validateJob(ctx, projectID, &jobID); err != nil {
			return nil, err
		}
		rules, err = s.repo.ListRulesByJob(ctx, projectID, jobID)
	}
	if err != nil {
		return nil, err
	}
	text, err := formatMonitorValue(value)
	if err != nil {
		return nil, err
	}
	result := &monitor.CheckResult{Key: key, CurrentValue: text, Rules: make([]monitor.RuleStatus, 0)}
	for _, rule := range rules {
		if rule.Key == key {
			result.Rules = append(result.Rules, monitor.EvaluateValue(rule, value))
		}
	}
	result.Evaluated = len(result.Rules) > 0
	result.Violated = monitor.HasViolation(result.Rules)
	return result, nil
}

func (s *MonitorService) validateJob(ctx context.Context, projectID string, jobID *string) error {
	if jobID == nil {
		return nil
	}
	exists, err := s.repo.JobBelongsToProject(ctx, projectID, *jobID)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("job não encontrado neste projeto")
	}
	return nil
}

func formatMonitorValue(value any) (string, error) {
	if text, ok := value.(string); ok {
		return text, nil
	}
	raw, err := json.Marshal(value)
	return string(raw), err
}

// extractJSONField extrai o valor de um campo JSON usando notação por pontos (ex: "data.accountsOnline" ou "goroutines_count").
func extractJSONField(data map[string]any, path string) (string, bool) {
	value, found := monitor.ExtractValue(data, path)
	if !found {
		return "", false
	}
	if value == nil {
		return "", true
	}
	text, err := formatMonitorValue(value)
	return text, err == nil
}

// ListRulesByJob retorna todas as regras ativas de um job ou globais.
func (s *MonitorService) ListRulesByJob(ctx context.Context, projectID, jobID string) ([]*monitor.MonitorRule, error) {
	return s.repo.ListRulesByJob(ctx, projectID, jobID)
}

// evaluateRule verifica se o currentValue viola a regra definida por (operator, threshold).
// Retorna (true, msg) se houver VIOLAÇÃO da regra.
func evaluateRule(currentVal string, op monitor.Operator, threshold string) (bool, string) {
	var value any = currentVal
	if monitor.NumericOperator(op) {
		value = json.Number(currentVal)
	}
	status := monitor.EvaluateValue(&monitor.MonitorRule{Operator: op, ThresholdValue: threshold}, value)
	return !status.Passed, status.Message
}

func (s *MonitorService) validateRule(ctx context.Context, rule *monitor.MonitorRule) error {
	if rule.Mode == "" {
		rule.Mode = monitor.ModeObserve
	}
	if rule.ValueType == "" {
		rule.ValueType = "legacy"
	}
	if strings.TrimSpace(rule.Name) == "" || len(rule.Name) > 100 || strings.TrimSpace(rule.Key) == "" || len(rule.Key) > 100 {
		return fmt.Errorf("nome e campo são obrigatórios e devem ter até 100 caracteres")
	}
	if rule.Mode != monitor.ModeObserve && rule.Mode != monitor.ModeRequireSuccess {
		return fmt.Errorf("intenção de condição inválida")
	}
	if rule.Mode == monitor.ModeRequireSuccess && rule.JobID == nil {
		return fmt.Errorf("condição de sucesso precisa estar vinculada a um job")
	}
	switch rule.Operator {
	case monitor.OpGt, monitor.OpGte, monitor.OpLt, monitor.OpLte, monitor.OpEq, monitor.OpNe, monitor.OpContains:
	default:
		return fmt.Errorf("operador inválido")
	}
	switch rule.ValueType {
	case "legacy":
		if rule.ThresholdValue == "" {
			return fmt.Errorf("valor esperado obrigatório")
		}
	case "number":
		if rule.Operator == monitor.OpContains || !monitor.ValidNumericThreshold(rule.ThresholdValue) {
			return fmt.Errorf("informe um número válido e um operador numérico")
		}
	case "boolean":
		if (rule.Operator != monitor.OpEq && rule.Operator != monitor.OpNe) || (rule.ThresholdValue != "true" && rule.ThresholdValue != "false") {
			return fmt.Errorf("condição booleana exige igualdade/diferença e true ou false")
		}
	case "null":
		if (rule.Operator != monitor.OpEq && rule.Operator != monitor.OpNe) || rule.ThresholdValue != "null" {
			return fmt.Errorf("condição null exige igualdade/diferença e valor null")
		}
	case "string":
		if monitor.NumericOperator(rule.Operator) {
			return fmt.Errorf("use operadores de texto para campos de texto")
		}
	default:
		return fmt.Errorf("tipo esperado inválido")
	}
	if monitor.NumericOperator(rule.Operator) && !monitor.ValidNumericThreshold(rule.ThresholdValue) {
		return fmt.Errorf("o limite deve ser um número JSON válido")
	}
	return s.validateJob(ctx, rule.ProjectID, rule.JobID)
}

// PreviewRule evaluates a candidate against a sample without saving or alerting.
func (s *MonitorService) PreviewRule(ctx context.Context, rule *monitor.MonitorRule, body []byte) ([]monitor.RuleStatus, error) {
	if rule.Name == "" {
		rule.Name = "Prévia"
	}
	if err := s.validateRule(ctx, rule); err != nil {
		return nil, err
	}
	if len(body) > 256*1024 {
		return nil, fmt.Errorf("amostra excede 256 KiB")
	}
	return monitor.EvaluateResponse([]*monitor.MonitorRule{rule}, monitor.Response{Received: true, Body: body}), nil
}
