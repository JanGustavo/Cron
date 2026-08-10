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
	agentHandler *handler.AgentHandler,
	pixHandler *handler.PixHandler,
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
	r.Get("/v1/health", healthHandler.Check)
	r.Get("/swagger/*", httpSwagger.WrapHandler)
	r.Post("/v1/auth/signup", authHandler.Signup)
	r.Post("/v1/auth/login", authHandler.Login)
	r.Post("/v1/auth/forgot-password", authHandler.ForgotPassword)
	r.Post("/v1/auth/reset-password", authHandler.ResetPassword)
	r.Get("/v1/auth/oauth/google", authHandler.OAuthGoogle)
	r.Get("/v1/auth/oauth/google/callback", authHandler.OAuthGoogleCallback)
	r.Get("/v1/auth/oauth/github", authHandler.OAuthGitHub)
	r.Get("/v1/auth/oauth/github/callback", authHandler.OAuthGitHubCallback)

	// Rotas autenticadas
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(userRepo, jwtSecret))

		r.Post("/v1/agent/chat", agentHandler.Chat)
		r.Post("/v1/projects/webhook-secret/rotate", authHandler.RotateWebhookSecret)
		r.Get("/v1/jobs/{id}/executions", executionHandler.List)
		r.Get("/v1/executions", executionHandler.ListGlobal)

		r.Get("/v1/pix/valores", pixHandler.ListValores)
		r.Get("/v1/pix/qr", pixHandler.GenerateQR)

		r.Route("/v1/keys", func(r chi.Router) {
			r.Get("/", authHandler.ListAPIKeys)
			r.Post("/", authHandler.CreateAPIKey)
			r.Delete("/{id}", authHandler.DeleteAPIKey)
		})

		r.Route("/v1/jobs", func(r chi.Router) {
			r.Get("/", jobHandler.List)
			r.Post("/", jobHandler.Create)
			r.Post("/{id}/trigger", jobHandler.TriggerNow)
			r.Get("/{id}", jobHandler.GetByID)
			r.Patch("/{id}", jobHandler.UpdateStatus)
			r.Put("/{id}", jobHandler.Update)
			r.Delete("/{id}", jobHandler.Delete)
		})

	})

	return r
}
