package router

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/JanGustavo/Cron/internal/api/middleware"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
)

// NewRouter monta todas as rotas da API REST usando chi e injeta as dependências necessárias.
func NewRouter(db *sql.DB, userRepo *postgres.UserRepository) *chi.Mux {
	r := chi.NewRouter()

	// Middlewares globais
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RealIP)

	// Rota raiz (Healthcheck simples)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"service": "cronflow-api",
			"health":  "/health",
		})
	})

	// Healthcheck completo
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := db.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"status":   "error",
				"postgres": "down",
				"error":    err.Error(),
			})
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status":   "ok",
			"postgres": "up",
		})
	})

	// Rotas da versão 1 (V1)
	r.Route("/v1", func(r chi.Router) {
		// Rotas protegidas pelo middleware de autenticação (API Key)
		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(userRepo))

			// Exemplo de rota de jobs cadastrada para teste de autenticação
			r.Get("/jobs", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"message": "Autenticado com sucesso!",
					"jobs":    []interface{}{},
				})
			})

			// Outras rotas protegidas virão aqui...
		})
	})

	return r
}

