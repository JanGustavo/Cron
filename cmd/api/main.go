package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/JanGustavo/Cron/internal/config"
	"github.com/JanGustavo/Cron/internal/database"
)

func main() {
	// 1. carrega as configurações
	cfg := config.Load()

	// 2. conecta ao banco
	db, err := database.ConnectPostgres(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Erro ao conectar ao banco de dados: %v", err)
	}

	// configura o router
	defer db.Close()

	// 3. monta o router
	r := chi.NewRouter()
	r.Use(middleware.Logger)    // loga cada request
	r.Use(middleware.Recoverer) // previne crashes

	// 4. rotas
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		//testa a conexão com o banco
		if err := db.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"status":   "error",
				"postgres": "down",
				"error":    err.Error(),
			})
			return
		}

		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status":   "ok",
			"postgres": "up",
			"env":      cfg.AppEnv,
		})
	})

	// 5. sobe o servidor
	log.Printf("Api rodando na porta %s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatalf("Falha ao iniciar servidor %v", err)
	}
}
