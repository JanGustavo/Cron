package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
)

// Response describes capture separately from the stored log preview.
type Response struct {
	Body         []byte
	Received     bool
	CaptureError string
}

// EvaluateResponse is pure: it neither persists samples nor dispatches alerts.
func EvaluateResponse(rules []*MonitorRule, response Response) []RuleStatus {
	results := make([]RuleStatus, 0, len(rules))
	var payload any
	reason, message, state := "", "", "fail"
	switch {
	case !response.Received:
		reason, message, state = "no_http_response", "Sem resposta HTTP para avaliar", "skipped"
	case response.CaptureError != "":
		reason, message, state = response.CaptureError, "Não foi possível capturar o corpo completo da resposta", "error"
	default:
		decoder := json.NewDecoder(bytes.NewReader(response.Body))
		decoder.UseNumber()
		err := decoder.Decode(&payload)
		var extra any
		if err != nil || decoder.Decode(&extra) != io.EOF {
			reason, message = "invalid_json", "A resposta não contém um documento JSON válido"
		}
	}
	for _, rule := range rules {
		if reason != "" {
			results = append(results, result(rule, state, reason, message))
			continue
		}
		value, found := ExtractValue(payload, rule.Key)
		if !found {
			results = append(results, result(rule, "fail", "missing_field", fmt.Sprintf("Campo '%s' não encontrado no corpo da resposta", rule.Key)))
			continue
		}
		results = append(results, EvaluateValue(rule, value))
	}
	return results
}

func result(rule *MonitorRule, state, reason, message string) RuleStatus {
	return RuleStatus{Mode: rule.Mode, ValueType: rule.ValueType, RuleID: rule.ID, RuleName: rule.Name, Key: rule.Key, Operator: rule.Operator,
		ExpectedValue: rule.ThresholdValue, Status: state, Reason: reason, Passed: state == "pass", Message: message}
}

// ExtractValue supports the existing dotted paths and numeric array segments.
func ExtractValue(payload any, path string) (any, bool) {
	current := payload
	parts := strings.Split(path, ".")
	if strings.HasPrefix(path, "/") {
		parts = strings.Split(path[1:], "/")
		for i, part := range parts {
			parts[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		}
	}
	for _, part := range parts {
		switch node := current.(type) {
		case map[string]any:
			value, exists := node[part]
			if !exists {
				return nil, false
			}
			current = value
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(node) {
				return nil, false
			}
			current = node[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func NumericOperator(op Operator) bool {
	return op == OpGt || op == OpGte || op == OpLt || op == OpLte
}

// Decimal comparisons avoid float64 rounding of JSON integers and thresholds.
func number(value any) (*big.Rat, bool) {
	var text string
	switch v := value.(type) {
	case json.Number:
		text = v.String()
	case float64:
		text = strconv.FormatFloat(v, 'g', -1, 64)
	default:
		return nil, false
	}
	// Restrict the accepted syntax to a JSON number, including exponent notation.
	if len(text) == 0 || len(text) > 128 || !json.Valid([]byte(text)) || (text[0] != '-' && (text[0] < '0' || text[0] > '9')) {
		return nil, false
	}
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		exponent, err := strconv.Atoi(text[index+1:])
		if err != nil || exponent < -1000 || exponent > 1000 {
			return nil, false
		}
	}
	n, ok := new(big.Rat).SetString(text)
	return n, ok
}

func ValidNumericThreshold(text string) bool {
	_, ok := number(json.Number(text))
	return ok
}

// Legacy thresholds are textual. Their type follows the observed scalar for
// equality; numeric ordering always requires an actual JSON number.
func EvaluateValue(rule *MonitorRule, value any) RuleStatus {
	status := result(rule, "pass", "", "")
	status.ObservedValue, _ = json.Marshal(value)
	status.ObservedValueText = string(status.ObservedValue)
	fail := func(state, reason, message string) RuleStatus {
		status.Status, status.Reason, status.Message, status.Passed = state, reason, message, false
		return status
	}
	if rule.ValueType != "" && rule.ValueType != "legacy" {
		actual := ""
		switch value.(type) {
		case string:
			actual = "string"
		case bool:
			actual = "boolean"
		case nil:
			actual = "null"
		case json.Number, float64:
			actual = "number"
		}
		if actual != rule.ValueType {
			return fail("fail", "type_mismatch", fmt.Sprintf("Esperado tipo %s, recebido %s", rule.ValueType, actual))
		}
	}
	matches := false
	switch {
	case NumericOperator(rule.Operator):
		current, ok := number(value)
		if !ok {
			return fail("fail", "type_mismatch", "A condição numérica recebeu um valor que não é número")
		}
		expected, ok := number(json.Number(rule.ThresholdValue))
		if !ok {
			return fail("error", "invalid_expectation", "O limite da condição deve ser um número JSON válido")
		}
		cmp := current.Cmp(expected)
		switch rule.Operator {
		case OpGt:
			matches = cmp > 0
		case OpGte:
			matches = cmp >= 0
		case OpLt:
			matches = cmp < 0
		case OpLte:
			matches = cmp <= 0
		}
	case rule.Operator == OpContains:
		text, ok := value.(string)
		if !ok {
			return fail("fail", "type_mismatch", "A condição de texto recebeu um valor que não é texto")
		}
		matches = strings.Contains(text, rule.ThresholdValue)
	case rule.Operator == OpEq || rule.Operator == OpNe:
		switch v := value.(type) {
		case string:
			matches = v == rule.ThresholdValue
		case bool:
			if rule.ThresholdValue != "true" && rule.ThresholdValue != "false" {
				return fail("error", "invalid_expectation", "O valor esperado deve ser true ou false")
			}
			matches = strconv.FormatBool(v) == rule.ThresholdValue
		case nil:
			matches = rule.ThresholdValue == "null"
		case json.Number, float64:
			current, ok := number(value)
			expected, valid := number(json.Number(rule.ThresholdValue))
			if !ok || !valid {
				return fail("error", "invalid_expectation", "O valor esperado deve ser um número JSON válido")
			}
			matches = current.Cmp(expected) == 0
		default:
			return fail("fail", "type_mismatch", "Selecione um campo escalar para esta condição")
		}
		if rule.Operator == OpNe {
			matches = !matches
		}
	default:
		return fail("error", "invalid_operator", "Operador de monitoramento inválido")
	}
	if !matches {
		return fail("fail", "condition_not_met", fmt.Sprintf("Valor observado %s não atende à condição %s %s", status.ObservedValue, rule.Operator, rule.ThresholdValue))
	}
	return status
}

func HasViolation(results []RuleStatus) bool {
	for _, status := range results {
		if status.Status == "fail" || status.Status == "error" {
			return true
		}
	}
	return false
}

// SuccessConditionsPassed fails closed on required errors or skipped evaluations.
func SuccessConditionsPassed(results []RuleStatus) bool {
	for _, status := range results {
		if status.Mode == ModeRequireSuccess && status.Status != "pass" {
			return false
		}
		if status.Reason == "evaluation_unavailable" {
			return false
		}
	}
	return true
}
