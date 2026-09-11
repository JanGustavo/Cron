package monitor

import "time"

type Operator string

const (
	OpGt       Operator = "gt"       // >
	OpGte      Operator = "gte"      // >=
	OpLt       Operator = "lt"       // <
	OpLte      Operator = "lte"      // <=
	OpEq       Operator = "eq"       // ==
	OpNe       Operator = "ne"       // !=
	OpContains Operator = "contains" // Contém texto
)

type MonitorRule struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	JobID          *string   `json:"job_id,omitempty"` // Opcional: vincula a um job específico
	Name           string    `json:"name"`
	Key            string    `json:"key"`
	Operator       Operator  `json:"operator"`
	ThresholdValue string    `json:"threshold_value"`
	AlertEmail     bool      `json:"alert_email"`
	WebhookURL     *string   `json:"webhook_url,omitempty"`
	IsEnabled      bool      `json:"is_enabled"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type KeyValue struct {
	ProjectID    string    `json:"project_id"`
	Key          string    `json:"key"`
	CurrentValue string    `json:"current_value"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CheckResult struct {
	Key          string       `json:"key"`
	CurrentValue string       `json:"current_value"`
	Evaluated    bool         `json:"evaluated"`
	Violated     bool         `json:"violated"`
	Rules        []RuleStatus `json:"rules"`
}

type RuleStatus struct {
	RuleID   string `json:"rule_id"`
	RuleName string `json:"rule_name"`
	Passed   bool   `json:"passed"`
	Message  string `json:"message,omitempty"`
}
