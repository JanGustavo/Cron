package execution

// Domain Entity: Execution.
// Responsabilidade: representar uma tentativa de execução de um Job.
// Cada vez que um Worker dispara um HTTP request, uma Execution é criada.
import "time"

type Status string

const (
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
	StatusTimeout Status = "timeout"
)

type Execution struct {
	ID              string     `json:"id"`
	JobID           string     `json:"job_id"`
	Status          Status     `json:"status"`
	HTTPStatus      *int       `json:"http_status,omitempty"`
	DurationMs      int        `json:"duration_ms"`
	ResponseBody    string     `json:"response_body,omitempty"`
	AttemptNumber   int        `json:"attempt_number"`
	TriggeredAt     time.Time  `json:"triggered_at"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	RuleEvaluations any        `json:"rule_evaluations,omitempty"`
}

type ProjectExecution struct {
	Execution
	JobName string `json:"job_name"`
	JobURL  string `json:"job_url"`
}

func (e *Execution) isSuccess() bool {
	return e.Status == StatusSuccess
}

const MaxResponseBodySize = 2 * 1024
