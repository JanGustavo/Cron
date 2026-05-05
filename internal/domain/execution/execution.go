package execution

// Domain Entity: Execution.
// Responsabilidade: representar uma tentativa de execução de um Job.
// Cada vez que um Worker dispara um HTTP request, uma Execution é criada.

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
	HTTPStatus    int
	DurationMs    int
	ResponseBody  string // truncado em 2KB
	AttemptNumber int
}
