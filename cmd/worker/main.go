package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"

	"github.com/JanGustavo/Cron/internal/config"
	"github.com/JanGustavo/Cron/internal/database"
	"github.com/JanGustavo/Cron/internal/queue"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/service"
	"github.com/JanGustavo/Cron/internal/worker"
)

func main() {
	cfg := config.Load()

	db, err := database.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Worker: falha ao conectar no banco: %v", err)
	}
	defer db.Close()

	// Repositories e services
	jobRepo := postgres.NewJobRepository(db)
	executionRepo := postgres.NewExecutionRepository(db)
	alertService := service.NewAlertService()

	// Worker handler
	w := worker.New(jobRepo, executionRepo, alertService)

	// Servidor Asynq — consome a fila Redis
	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: cfg.RedisURL},
		asynq.Config{
			Concurrency: 50,
			Queues: map[string]int{
				"critical": 6, // plano pago (futuro)
				"default":  3, // plano free
				"low":      1,
			},
			// Backoff exponencial: tentativa 1=1min, 2=5min, 3=15min
			RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
				delays := []time.Duration{1 * time.Minute, 5 * time.Minute, 15 * time.Minute}
				if n > 0 && n-1 < len(delays) {
					return delays[n-1]
				}
				return 15 * time.Minute
			},
		},
	)

	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TypeHTTPJob, w.ProcessTask)

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Println("Worker iniciado — aguardando tasks na fila Redis")
	// Use Run with signal context if supported, or run synchronously and shut down on context done.
	// Asynq's srv.Run(handler) blocks until SIGINT/SIGTERM, which fits perfect!
	if err := srv.Run(mux); err != nil {
		log.Fatalf("Worker: erro ao iniciar servidor Asynq: %v", err)
	}
	_ = ctx // avoid declared and not used if needed, though we used context.Background() above
}
