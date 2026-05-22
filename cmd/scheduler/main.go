package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/JanGustavo/Cron/internal/config"
	"github.com/JanGustavo/Cron/internal/database"
	"github.com/JanGustavo/Cron/internal/queue"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/scheduler"
)

func main() {
	cfg := config.Load()

	db, err := database.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Scheduler: falha ao conectar no banco: %v", err)
	}
	defer db.Close()

	enqueuer := queue.NewEnqueuer(cfg.RedisURL)
	defer enqueuer.Close()

	jobRepo := postgres.NewJobRepository(db)

	sched := scheduler.New(jobRepo, enqueuer, 30*time.Second)

	// Graceful shutdown: Ctrl+C ou SIGTERM encerra o loop limpo
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	sched.Run(ctx)
}
