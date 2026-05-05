package queue

// Enqueuer — abstração sobre Asynq para enfileirar tasks.
// Encapsula o Asynq para que o Scheduler não conheça detalhes do Redis.
// Define o tipo da task e payload JSON.
// Aplica fila correta por plano: critical (paid) / default (free).

const TypeHTTPJob = "http:job"

type HTTPJobPayload struct {
	JobID string `json:"job_id"`
}

type Enqueuer struct{}
