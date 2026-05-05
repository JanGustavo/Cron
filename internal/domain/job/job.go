package job

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

type Job struct {
	ID                  string
	ProjectID           string
	Name                string
	Schedule            string
	Timezone            string
	URL                 string
	HTTPMethod          string
	Status              Status
	NextRunAt           interface{}
	ConsecutiveFailures int
}
