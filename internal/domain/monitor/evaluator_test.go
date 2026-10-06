package monitor

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResponseEvaluation(t *testing.T) {
	cases := []struct {
		name, body, key         string
		op                      Operator
		expected, state, reason string
		received                bool
		capture                 string
	}{
		{"healthy bool", `{"data":{"synced":true}}`, "data.synced", OpEq, "true", "pass", "", true, ""},
		{"business failure", `{"data":{"synced":false}}`, "data.synced", OpEq, "true", "fail", "condition_not_met", true, ""},
		{"missing field", `{}`, "success", OpEq, "true", "fail", "missing_field", true, ""},
		{"invalid JSON", `{"success":`, "success", OpEq, "true", "fail", "invalid_json", true, ""},
		{"empty response", ``, "success", OpEq, "true", "fail", "invalid_json", true, ""},
		{"trailing document", `{"success":true} {}`, "success", OpEq, "true", "fail", "invalid_json", true, ""},
		{"null is present", `{"success":null}`, "success", OpEq, "null", "pass", "", true, ""},
		{"zero is present", `{"count":0}`, "count", OpGte, "0", "pass", "", true, ""},
		{"empty text", `{"status":""}`, "status", OpEq, "", "pass", "", true, ""},
		{"numeric text rejected", `{"count":"100"}`, "count", OpGt, "99", "fail", "type_mismatch", true, ""},
		{"boolean ordering rejected", `{"count":true}`, "count", OpGt, "99", "fail", "type_mismatch", true, ""},
		{"precise integer", `{"count":9007199254740993}`, "count", OpEq, "9007199254740992", "fail", "condition_not_met", true, ""},
		{"numeric equality", `{"count":1}`, "count", OpEq, "1.0", "pass", "", true, ""},
		{"arrays", `{"items":[{"status":"ok"}]}`, "items.0.status", OpEq, "ok", "pass", "", true, ""},
		{"root arrays", `[false,true]`, "1", OpEq, "true", "pass", "", true, ""},
		{"null root", `null`, "status", OpEq, "ok", "fail", "missing_field", true, ""},
		{"contains scalar only", `{"status":{"ok":true}}`, "status", OpContains, "ok", "fail", "type_mismatch", true, ""},
		{"invalid numeric threshold", `{"count":1}`, "count", OpGt, "NaN", "error", "invalid_expectation", true, ""},
		{"oversized capture", `{"count":1}`, "count", OpEq, "1", "error", "body_too_large", true, "body_too_large"},
		{"read failure", `{"count":1}`, "count", OpEq, "1", "error", "body_read_error", true, "body_read_error"},
		{"network failure", ``, "count", OpEq, "1", "skipped", "no_http_response", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := &MonitorRule{ID: "rule-1", Name: "Condition", Key: tc.key, Operator: tc.op, ThresholdValue: tc.expected}
			results := EvaluateResponse([]*MonitorRule{rule}, Response{Body: []byte(tc.body), Received: tc.received, CaptureError: tc.capture})
			if len(results) != 1 || results[0].Status != tc.state || results[0].Reason != tc.reason {
				t.Fatalf("unexpected evaluation: %+v", results)
			}
			if results[0].Passed != (tc.state == "pass") {
				t.Fatal("legacy passed disagrees with explicit status")
			}
			if HasViolation(results) != (tc.state == "fail" || tc.state == "error") {
				t.Fatal("aggregate disagrees with condition")
			}
			if results[0].RuleID != rule.ID || results[0].ExpectedValue != tc.expected {
				t.Fatal("condition snapshot missing")
			}
		})
	}
}

func TestEvaluateCompleteBodyBeyondLogPreview(t *testing.T) {
	body := `{"padding":"` + strings.Repeat("x", 4096) + `","success":false}`
	results := EvaluateResponse([]*MonitorRule{{Key: "success", Operator: OpEq, ThresholdValue: "true"}}, Response{Received: true, Body: []byte(body)})
	if len(results) != 1 || results[0].Reason != "condition_not_met" || string(results[0].ObservedValue) != "false" {
		t.Fatalf("full response not evaluated: %+v", results)
	}
}

func TestNumericLimitsAndPrecision(t *testing.T) {
	for _, invalid := range []string{"NaN", "Inf", "1e1001", "1e-1001", "1e99999999999999999", "01", "true", strings.Repeat("9", 129)} {
		if ValidNumericThreshold(invalid) {
			t.Errorf("accepted unsafe or invalid numeric threshold %q", invalid)
		}
	}
	for _, valid := range []string{"0", "-12.5", "1e3", "0.0000000001", "9007199254740993"} {
		if !ValidNumericThreshold(valid) {
			t.Errorf("rejected number %s", valid)
		}
	}
	result := EvaluateValue(&MonitorRule{Operator: OpGt, ThresholdValue: "9007199254740992"}, json.Number("9007199254740993"))
	if !result.Passed {
		t.Fatalf("lost integer precision: %+v", result)
	}
}

func TestNoRulesDoesNotClaimEvaluation(t *testing.T) {
	results := EvaluateResponse(nil, Response{Received: true, Body: []byte("invalid")})
	if len(results) != 0 || HasViolation(results) {
		t.Fatal("unconfigured job has invented conditions")
	}
}

func TestTypedFieldsAndJSONPointer(t *testing.T) {
	for _, tc := range []struct{ key, body, typ, expected, status string }{
		{"/a.b/x~1y/~0/", `{"a.b":{"x/y":{"~":{"":false}}}}`, "boolean", "false", "pass"},
		{"/items/0/count", `{"items":[{"count":9007199254740993}]}`, "number", "9007199254740993", "pass"},
		{"/value", `{"value":"true"}`, "boolean", "true", "fail"},
		{"/value", `{"value":1}`, "string", "1", "fail"},
		{"/value", `{"value":""}`, "string", "", "pass"},
	} {
		result := EvaluateResponse([]*MonitorRule{{Key: tc.key, ValueType: tc.typ, Mode: ModeRequireSuccess, Operator: OpEq, ThresholdValue: tc.expected}}, Response{Received: true, Body: []byte(tc.body)})
		if len(result) != 1 || result[0].Status != tc.status || SuccessConditionsPassed(result) != (tc.status == "pass") {
			t.Fatalf("typed pointer evaluation: %+v", result)
		}
	}
	for _, status := range []string{"fail", "error", "skipped"} {
		if SuccessConditionsPassed([]RuleStatus{{Mode: ModeRequireSuccess, Status: status}}) {
			t.Errorf("required %s allowed success", status)
		}
		if !SuccessConditionsPassed([]RuleStatus{{Mode: ModeObserve, Status: status}}) {
			t.Errorf("observe %s blocked success", status)
		}
	}
}
