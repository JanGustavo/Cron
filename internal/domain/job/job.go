package job

import "time"

// Domain Entity: Job.
// Responsabilidade: definir a estrutura central do sistema.
// Contém o tipo Job, constantes de status e regras de negócio puras
// que não dependem de banco ou HTTP.
// ZERO dependências de pacotes externos. Só stdlib.

type Status string

const (
	StatusActive  Status = "active"
	StatusPaused  Status = "paused"
	StatusFailing Status = "failing"
)

type HTTPMethod string

const (
	MethodGet  HTTPMethod = "GET"
	MethodPost HTTPMethod = "POST"
)

// internal/domain/job/job.go
type Job struct {
	ID                  string            `json:"id"`
	ProjectID           string            `json:"project_id"`
	Name                string            `json:"name"`
	Schedule            string            `json:"schedule"`
	Timezone            string            `json:"timezone"` // "AMERICA/Sao_Paulo", "UTC", etc
	URL                 string            `json:"url"`
	HTTPMethod          HTTPMethod        `json:"http_method"`
	Headers             map[string]string `json:"headers,omitempty"`// {"Content-Type": "application/json"}
	Payload             map[string]any    `json:"payload,omitempty"` // corpo JSON
	Status              Status            `json:"status"`
	NextRunAt           time.Time         `json:"next_run_at"`
	LastRunAt           *time.Time        `json:"last_run_at,omitempty"` // se nunca rodou, é nil
	ConsecutiveFailures int               `json:"consecutive_failures"`
	WebhookAlertURL     *string           `json:"webhook_alert_url,omitempty"` // URL para callbacks de sucesso/falha
	NextJobID           *string           `json:"next_job_id,omitempty"`
	Tags                []string          `json:"tags,omitempty"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

// regras de negócio puras, sem dependências externas:
// retorna true se o job estiver ativo e a próxima execução for no passado ou agora
func (j *Job) IsElegibleToRun(now time.Time) bool {
	return j.Status == StatusActive && !j.NextRunAt.After(now)
}

// verifica se o job deve disparar o webhook de alerta após 3 falhas consecutivas
func (j *Job) ShouldAlert() bool {
	return j.ConsecutiveFailures >= 3 && j.WebhookAlertURL != nil
}

// aumenta o contador de falhas consecutivas e, se atingir 3, marca o job como "failing"
func (j *Job) markAsFailed() {
	j.ConsecutiveFailures++
	if j.ConsecutiveFailures >= 3 {
		j.Status = StatusFailing
	}
}

// reseta o contador de falhas e volta o status para "active"
func (j *Job) ResetFailures() {
	j.ConsecutiveFailures = 0
	j.Status = StatusActive //apo
}
