package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/JanGustavo/Cron/internal/api/handler"
	"github.com/JanGustavo/Cron/internal/api/middleware"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/service"
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
	metricsHandler *handler.MetricsHandler,
	billingHandler *handler.BillingHandler,
	adminHandler *handler.AdminHandler,
	entitlementEngine *service.EntitlementEngine,
	jwtSecret string,
) *chi.Mux {
	r := chi.NewRouter()

	// Middlewares globais
	r.Use(middleware.RateLimit(300))
	r.Use(middleware.CORS)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RealIP)

	// Rotas publicas — sem autenticacao
	r.Get("/health", healthHandler.Check)
	r.Head("/health", healthHandler.Check)
	r.Get("/v1/health", healthHandler.Check)
	r.Head("/v1/health", healthHandler.Check)
	r.Get("/v1/health/ai", healthHandler.CheckAI)
	r.Get("/swagger/*", httpSwagger.WrapHandler)
	r.Post("/v1/auth/signup", authHandler.Signup)
	r.Post("/v1/auth/verify-email", authHandler.VerifyEmail)
	r.Post("/v1/auth/resend-verification", authHandler.ResendVerification)
	r.Post("/v1/auth/login", authHandler.Login)
	r.Post("/v1/auth/forgot-password", authHandler.ForgotPassword)
	r.Post("/v1/auth/reset-password", authHandler.ResetPassword)
	r.Get("/v1/auth/oauth/google", authHandler.OAuthGoogle)
	r.Get("/v1/auth/oauth/google/callback", authHandler.OAuthGoogleCallback)
	r.Get("/v1/auth/oauth/github", authHandler.OAuthGitHub)
	r.Get("/v1/auth/oauth/github/callback", authHandler.OAuthGitHubCallback)

	// Webhook Stripe publico
	r.Post("/v1/billing/webhook", billingHandler.Webhook)

	// Webhooks de Teste Locais (Bypass de dependência Python)
	r.Post("/webhook-mock-5001", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success","received_at_mock":5001}`))
	})
	r.Post("/webhook-mock-5002", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success","received_at_mock":5002}`))
	})


	// Rotas autenticadas
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(userRepo, jwtSecret))

		// Rotas Administrativas (Apenas Admin)
		r.Route("/v1/admin", func(r chi.Router) {
			r.Use(middleware.RequireAdmin(userRepo))
			r.Get("/me", adminHandler.CheckCurrentAdminRole)
			r.Get("/users", adminHandler.ListUsers)
			r.Put("/users/{id}/plan", adminHandler.UpdateUserPlan)
			r.Post("/users/{id}/reset-ai", adminHandler.ResetUserAIQuota)
			r.Delete("/users/{id}", adminHandler.DeleteUser)
			r.Get("/stats", adminHandler.GetSystemStats)
		})

		r.Post("/v1/agent/chat", agentHandler.Chat)
		r.Post("/v1/projects/webhook-secret/rotate", authHandler.RotateWebhookSecret)
		r.Get("/v1/users/profile", authHandler.GetProfile)
		r.Put("/v1/users/profile", authHandler.UpdateProfile)
		r.Post("/v1/projects", authHandler.CreateProject)
		r.Put("/v1/projects/{id}", authHandler.UpdateProject)
		r.Delete("/v1/projects/{id}", authHandler.DeleteProject)
		r.Post("/v1/projects/{id}/switch", authHandler.SwitchProject)
		r.Get("/v1/jobs/{id}/executions", executionHandler.List)
		r.Get("/v1/executions", executionHandler.ListGlobal)
		r.Get("/v1/metrics/queue", metricsHandler.QueueMetrics)
		r.Get("/v1/metrics/system", metricsHandler.SystemMetrics)

		r.Get("/v1/pix/valores", pixHandler.ListValores)
		r.Get("/v1/pix/qr", pixHandler.GenerateQR)

		// Stripe Billing Session Endpoints
		r.Post("/v1/billing/checkout", billingHandler.CreateCheckoutSession)
		r.Post("/v1/billing/portal", billingHandler.CreatePortalSession)

		r.Route("/v1/keys", func(r chi.Router) {
			r.Get("/", authHandler.ListAPIKeys)
			r.Post("/", authHandler.CreateAPIKey)
			r.Delete("/{id}", authHandler.DeleteAPIKey)
		})

		r.Route("/v1/jobs", func(r chi.Router) {
			r.Get("/", jobHandler.List)
			r.With(middleware.RequireJobEntitlement(entitlementEngine)).Post("/", jobHandler.Create)
			r.Post("/{id}/trigger", jobHandler.TriggerNow)
			r.Get("/{id}", jobHandler.GetByID)
			r.Patch("/{id}", jobHandler.UpdateStatus)
			r.Put("/{id}", jobHandler.Update)
			r.Delete("/{id}", jobHandler.Delete)
		})

	})

	return r
}
