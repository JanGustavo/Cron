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

type Job struct {
	ID                  string
	ProjectID           string
	Name                string
	Schedule            string
	Timezone            string // "AMERICA/Sao_Paulo", "UTC", etc
	URL                 string
	HTTPMethod          HTTPMethod
	Headers             map[string]string // {"Content-Type": "application/json"}
	Payload             string            // corpo
	Status              Status
	NextRunAt           time.Time
	LastRunAt 		    *time.Time // se nunca rodou, é nil	
	ConsecutiveFailures int
	WebhookAlertURL     *string // URL para callbacks de sucesso/falha
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

//regras de negócio puras, sem dependências externas:
//retorna true se o job estiver ativo e a próxima execução for no passado ou agora
func (j *Job) IsElegibleToRun(now time.Time) bool {
	return j.Status == StatusActive && !j.NextRunAt.After(now)	
}

//verifica se o job deve disparar o webhook de alerta após 3 falhas consecutivas
func (j *Job) ShouldAlert() bool {
	return j.ConsecutiveFailures >= 3 && j.WebhookAlertURL != nil 
} 

//aumenta o contador de falhas consecutivas e, se atingir 3, marca o job como "failing"
func (j *Job) markAsFailed() {
	j.ConsecutiveFailures++
	if j.ConsecutiveFailures >= 3 {
		j.Status = StatusFailing
	}
}

//reseta o contador de falhas e volta o status para "active"
func (j *Job) ResetFailures(){
	j.ConsecutiveFailures = 0
	j.Status = StatusActive //apo
}

