package main

import (
	"log"
	"net/http"

	"github.com/JanGustavo/Cron/internal/api/handler"
	"github.com/JanGustavo/Cron/internal/api/router"
	"github.com/JanGustavo/Cron/internal/config"
	"github.com/JanGustavo/Cron/internal/database"
	"github.com/JanGustavo/Cron/internal/queue"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/service"
)

// @title CronFlow API
// @version 1.0
// @description API do sistema CronFlow para agendamento e monitoramento de execução de tarefas distribuídas em background.
// @host localhost:8080
// @BasePath /

// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name Authorization
// @description Tipo "Bearer <sua-api-key>" para se autenticar.
func main() {
	// 1. carrega as configuracoes
	cfg := config.Load()

	// 2. conecta ao banco
	db, err := database.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Falha ao conectar no banco: %v", err)
	}
	defer db.Close()

	// Garante DDL do CPF e Nome Completo nos bancos local e prod
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS cpf VARCHAR(11) UNIQUE; ALTER TABLE users ADD COLUMN IF NOT EXISTS full_name TEXT;`)

	// Repositories
	userRepo := postgres.NewUserRepository(db)
	jobRepo := postgres.NewJobRepository(db)
	executionRepo := postgres.NewExecutionRepository(db)

	// queue
	enqueuer := queue.NewEnqueuer(cfg.RedisURL)
	defer enqueuer.Close()

	// Services
	jobService := service.NewJobService(jobRepo, userRepo, enqueuer, cfg)

	// Handlers
	healthHandler := handler.NewHealthHandler(db, cfg.RedisURL)
	jobHandler := handler.NewJobHandler(jobService)
	executionHandler := handler.NewExecutionHandler(jobService, executionRepo)
	authHandler := handler.NewAuthHandler(userRepo, cfg)
	agentHandler := handler.NewAgentHandler(jobService, cfg)
	pixHandler := handler.NewPixHandler()

	// Router
	r := router.New(userRepo, jobHandler, healthHandler, executionHandler, authHandler, agentHandler, pixHandler, cfg.JWTSecret)

	// 5. sobe o servidor
	log.Printf("API rodando em http://localhost:%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatalf("Falha ao iniciar servidor: %v", err)
	}

	queue.NewEnqueuer(cfg.RedisURL)

	
}
