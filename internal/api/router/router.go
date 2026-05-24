package router

import (
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/JanGustavo/Cron/internal/api/handler"
	"github.com/JanGustavo/Cron/internal/api/middleware"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	
)

func New(
	userRepo *postgres.UserRepository,
	jobHandler *handler.JobHandler,
	healthHandler *handler.HealthHandler,
	executionHandler *handler.ExecutionHandler,
) *chi.Mux {
	r := chi.NewRouter()

	// Middlewares globais
	r.Use(middleware.RateLimit(60))
	r.Use(middleware.CORS)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RealIP)

	// Rota publica — sem autenticacao
	r.Get("/health", healthHandler.Check)

	// Rotas autenticadas
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(userRepo))

		r.Get("/v1/jobs/{id}/executions", executionHandler.List)

		r.Route("/v1/jobs", func(r chi.Router) {
			r.Get("/", jobHandler.List)
			r.Post("/", jobHandler.Create)
			r.Post("/{id}/trigger", jobHandler.TriggerNow)
			r.Get("/{id}", jobHandler.GetByID)
			r.Patch("/{id}", jobHandler.UpdateStatus)
			r.Delete("/{id}", jobHandler.Delete)
		})

	})

	return r
}
