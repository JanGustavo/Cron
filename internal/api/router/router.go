package router

import (
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/JanGustavo/Cron/internal/api/handler"
	"github.com/JanGustavo/Cron/internal/api/middleware"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	_ "github.com/JanGustavo/Cron/docs"
	httpSwagger "github.com/swaggo/http-swagger/v2"
)

func New(
	userRepo *postgres.UserRepository,
	jobHandler *handler.JobHandler,
	healthHandler *handler.HealthHandler,
	executionHandler *handler.ExecutionHandler,
	authHandler *handler.AuthHandler,
	jwtSecret string,
) *chi.Mux {
	r := chi.NewRouter()

	// Middlewares globais
	r.Use(middleware.RateLimit(60))
	r.Use(middleware.CORS)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RealIP)

	// Rotas publicas — sem autenticacao
	r.Get("/health", healthHandler.Check)
	r.Get("/swagger/*", httpSwagger.WrapHandler)
	r.Post("/v1/auth/signup", authHandler.Signup)
	r.Post("/v1/auth/login", authHandler.Login)

	// Rotas autenticadas
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(userRepo, jwtSecret))

		r.Get("/v1/jobs/{id}/executions", executionHandler.List)
		r.Get("/v1/executions", executionHandler.ListGlobal)

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
