package queue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
)

// Enqueuer — abstração sobre Asynq para enfileirar tasks.
// Encapsula o Asynq para que o Scheduler não conheça detalhes do Redis.
// Define o tipo da task e payload JSON.
// Aplica fila correta por plano: critical (paid) / default (free).

const TypeHTTPJob = "http:job"

type HTTPJobPayload struct {
	JobID string `json:"job_id"`
}

type Enqueuer struct {
	client *asynq.Client
}

func NewEnqueuer(redisURL string) *Enqueuer {
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: redisURL})
	return &Enqueuer{client: client}
}

// Enqueue coloca um job na fila Redis para o Worker executar.
// Usa a fila "default" no MVP — em versões futuras: "critical" para plano pago.
func (e *Enqueuer) Enqueue(ctx context.Context, jobID string) error {
	payload, err := json.Marshal(HTTPJobPayload{JobID: jobID})
	if err != nil {
		return fmt.Errorf("Enqueuer.Enqueue: erro ao serializar payload: %w", err)
	}

	task := asynq.NewTask(TypeHTTPJob, payload)

	_, err = e.client.EnqueueContext(ctx, task,
		asynq.Queue("default"),
		asynq.MaxRetry(2),
	)
	if err != nil {
		return fmt.Errorf("Enqueuer.Enqueue: erro ao enfileirar job %s: %w", jobID, err)
	}

	return nil
}

// Close fecha a conexão com o Redis.
func (e *Enqueuer) Close() error {
	return e.client.Close()
}
