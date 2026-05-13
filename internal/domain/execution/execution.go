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
	ID            string
	JobID         string
	Status        Status
	HTTPStatus    *int // *int para permitir valor nil
	DurationMs    int
	ResponseBody  string // truncado em 2KB
	AttemptNumber int
	TriggeredAt   time.Time
	StartedAt     *time.Time
	FinishedAt    *time.Time
}

//retorna true se a execução recebeu resposta HTTP 
func (e *Execution) isSuccess() bool {
	return e.Status == StatusSuccess
}

//limite de 2KB para o corpo da resposta
const  MaxResponseBodySize = 2 * 1024 // 2KB

