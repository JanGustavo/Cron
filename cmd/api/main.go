package main

import (
	"log"
	"net/http"

	"github.com/JanGustavo/Cron/internal/api/router"
	"github.com/JanGustavo/Cron/internal/config"
	"github.com/JanGustavo/Cron/internal/database"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
)

func main() {
	// 1. carrega as configurações
	cfg := config.Load()

	// 2. conecta ao banco
	db, err := database.ConnectPostgres(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Erro ao conectar ao banco de dados: %v", err)
	}
	defer db.Close()

	// Inicializa os repositórios necessários
	userRepo := postgres.NewUserRepository(db)

	// 3. monta o router
	r := router.NewRouter(db, userRepo)

	// 5. sobe o servidor
	log.Printf("Api rodando na porta %s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatalf("Falha ao iniciar servidor %v", err)
	}
}
