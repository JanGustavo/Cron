package main

import (
	"log"
	"net/http"

	"github.com/JanGustavo/Cron/internal/api/handler"
	"github.com/JanGustavo/Cron/internal/api/router"
	"github.com/JanGustavo/Cron/internal/config"
	"github.com/JanGustavo/Cron/internal/database"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/service"
)

func main() {
	// 1. carrega as configuracoes
	cfg := config.Load()

	// 2. conecta ao banco
	db, err := database.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Falha ao conectar no banco: %v", err)
	}
	defer db.Close()

	// Repositories
	userRepo := postgres.NewUserRepository(db)
	jobRepo := postgres.NewJobRepository(db)
	executionRepo := postgres.NewExecutionRepository(db)

	// Services
	jobService := service.NewJobService(jobRepo, userRepo, cfg)

	// Handlers
	healthHandler := handler.NewHealthHandler(db)
	jobHandler := handler.NewJobHandler(jobService)
	executionHandler := handler.NewExecutionHandler(jobService, executionRepo)

	// Router
	r := router.New(userRepo, jobHandler, healthHandler, executionHandler)

	// 5. sobe o servidor
	log.Printf("API rodando em http://localhost:%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatalf("Falha ao iniciar servidor: %v", err)
	}
}
