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
	mailService := service.NewMailService(cfg.SmtpHost, cfg.SmtpPort, cfg.SmtpUser, cfg.SmtpPass, cfg.SmtpFrom)
	alertService := service.NewAlertService(db, mailService)

	enqueuer := queue.NewEnqueuer(cfg.RedisURL)
	defer enqueuer.Close()

	// Worker handler
	w := worker.New(jobRepo, executionRepo, alertService, enqueuer, cfg.JWTSecret)

	// Servidor Asynq — consome a fila Redis
	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: cfg.RedisURL},
		asynq.Config{
			Concurrency: cfg.WorkerConcurrency,
			Queues: map[string]int{
				"critical": 6, // plano pago (futuro)
				"default":  3, // plano free
				"low":      1,
			},
			// Backoff progressivo para as 3 retentativas (15s, 30s e 60s)
			RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
				delays := []time.Duration{15 * time.Second, 30 * time.Second, 60 * time.Second}
				if n >= 0 && n < len(delays) {
					return delays[n]
				}
				return 60 * time.Second
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
