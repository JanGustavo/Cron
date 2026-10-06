package monitor

import "testing"

func TestTypedOperatorMatrix(t *testing.T) {
	const payload = `{"id":1,"userId":1,"title":"delectus aut autem","completed":false,"nullable":null,"decimal":-1.5}`
	cases := []struct {
		field, typ       string
		op               Operator
		expected, status string
	}{
		{"/id", "number", OpEq, "1", "pass"}, {"/id", "number", OpEq, "2", "fail"},
		{"/id", "number", OpNe, "2", "pass"}, {"/id", "number", OpNe, "1", "fail"},
		{"/id", "number", OpGt, "0", "pass"}, {"/id", "number", OpGt, "1", "fail"},
		{"/id", "number", OpGte, "1", "pass"}, {"/id", "number", OpGte, "2", "fail"},
		{"/id", "number", OpLt, "2", "pass"}, {"/id", "number", OpLt, "1", "fail"},
		{"/id", "number", OpLte, "1", "pass"}, {"/id", "number", OpLte, "0", "fail"},
		{"/userId", "number", OpEq, "1.0", "pass"},
		{"/title", "string", OpEq, "delectus aut autem", "pass"}, {"/title", "string", OpEq, "DELECTUS AUT AUTEM", "fail"},
		{"/title", "string", OpNe, "outro título", "pass"}, {"/title", "string", OpNe, "delectus aut autem", "fail"},
		{"/title", "string", OpContains, "aut", "pass"}, {"/title", "string", OpContains, "inexistente", "fail"},
		{"/completed", "boolean", OpEq, "false", "pass"}, {"/completed", "boolean", OpEq, "true", "fail"},
		{"/completed", "boolean", OpNe, "true", "pass"}, {"/completed", "boolean", OpNe, "false", "fail"},
		{"/nullable", "null", OpEq, "null", "pass"}, {"/nullable", "null", OpNe, "null", "fail"},
		{"/decimal", "number", OpGt, "-2", "pass"}, {"/decimal", "number", OpLt, "-1", "pass"},
		{"/missing", "number", OpEq, "1", "fail"}, {"/id", "string", OpEq, "1", "fail"},
	}
	for _, tc := range cases {
		t.Run(tc.field+"/"+string(tc.op)+"/"+tc.expected, func(t *testing.T) {
			rules := []*MonitorRule{{Key: tc.field, ValueType: tc.typ, Mode: ModeRequireSuccess, Operator: tc.op, ThresholdValue: tc.expected}}
			got := EvaluateResponse(rules, Response{Body: []byte(payload), Received: true})
			if len(got) != 1 || got[0].Status != tc.status || SuccessConditionsPassed(got) != (tc.status == "pass") {
				t.Fatalf("unexpected result: %+v", got)
			}
		})
	}
}
