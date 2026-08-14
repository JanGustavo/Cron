package main

import (
	"log"
	"net/http"
	"net/url"

	"github.com/JanGustavo/Cron/docs"
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

	// Ajusta dinamicamente o Host e Schemes do Swagger a partir da API_URL
	if cfg.APIURL != "" {
		if u, err := url.Parse(cfg.APIURL); err == nil {
			docs.SwaggerInfo.Host = u.Host
			docs.SwaggerInfo.Schemes = []string{u.Scheme}
		}
	}

	// 2. conecta ao banco
	db, err := database.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Falha ao conectar no banco: %v", err)
	}
	defer db.Close()

	// Garante DDL do CPF e Nome Completo nos bancos local e prod
	_, _ = db.Exec(`
		ALTER TABLE users ADD COLUMN IF NOT EXISTS cpf VARCHAR(11) UNIQUE; 
		ALTER TABLE users ADD COLUMN IF NOT EXISTS full_name TEXT; 
		ALTER TABLE projects ADD COLUMN IF NOT EXISTS webhook_secret TEXT; 
		ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS email_alerts_enabled BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS daily_digest_enabled BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS timezone VARCHAR(50) NOT NULL DEFAULT 'America/Sao_Paulo';
		ALTER TABLE users ADD COLUMN IF NOT EXISTS digest_hour INT NOT NULL DEFAULT 18;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS last_digest_sent_at TIMESTAMPTZ;

		CREATE TABLE IF NOT EXISTS plans (
			code VARCHAR(50) PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			price_monthly INT NOT NULL DEFAULT 0,
			price_yearly INT NOT NULL DEFAULT 0,
			max_jobs INT NOT NULL DEFAULT 5,
			max_users INT NOT NULL DEFAULT 1,
			logs_retention_days INT NOT NULL DEFAULT 7,
			workflows_enabled BOOLEAN NOT NULL DEFAULT FALSE,
			alerts_webhooks_enabled BOOLEAN NOT NULL DEFAULT FALSE,
			multi_project_enabled BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS subscriptions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			plan_code VARCHAR(50) NOT NULL REFERENCES plans(code),
			status VARCHAR(30) NOT NULL DEFAULT 'trialing',
			billing_provider VARCHAR(50) NOT NULL DEFAULT 'stripe',
			provider_customer_id VARCHAR(100),
			provider_subscription_id VARCHAR(100),
			current_period_start TIMESTAMP WITH TIME ZONE,
			current_period_end TIMESTAMP WITH TIME ZONE,
			cancel_at_period_end BOOLEAN NOT NULL DEFAULT FALSE,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			UNIQUE(user_id)
		);

		CREATE TABLE IF NOT EXISTS billing_events (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			provider VARCHAR(32) NOT NULL,
			provider_event_id VARCHAR(255) NOT NULL,
			event_type VARCHAR(128) NOT NULL,
			user_id UUID REFERENCES users(id) ON DELETE SET NULL,
			payload JSONB NOT NULL,
			processed_at TIMESTAMP WITH TIME ZONE,
			processing_error TEXT,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			UNIQUE (provider, provider_event_id)
		);

		INSERT INTO plans (code, name, price_monthly, price_yearly, max_jobs, max_users, logs_retention_days, workflows_enabled, alerts_webhooks_enabled, multi_project_enabled)
		VALUES 
		('starter', 'Plano Starter', 0, 0, 5, 1, 7, FALSE, FALSE, FALSE),
		('pro', 'Plano Pro', 2900, 29000, 50, 3, 90, TRUE, TRUE, TRUE)
		ON CONFLICT (code) DO NOTHING;
	`)

	// Repositories
	userRepo := postgres.NewUserRepository(db)
	jobRepo := postgres.NewJobRepository(db)
	executionRepo := postgres.NewExecutionRepository(db)
	billingRepo := postgres.NewBillingRepository(db)

	// queue
	enqueuer := queue.NewEnqueuer(cfg.RedisURL)
	defer enqueuer.Close()

	// Services
	entitlementEngine := service.NewEntitlementEngine(billingRepo)
	jobService := service.NewJobService(jobRepo, userRepo, entitlementEngine, enqueuer, cfg)
	mailService := service.NewMailService(cfg.SmtpHost, cfg.SmtpPort, cfg.SmtpUser, cfg.SmtpPass, cfg.SmtpFrom)

	// Handlers
	healthHandler := handler.NewHealthHandler(db, cfg.RedisURL, cfg.AppEnv, cfg.SchedulerInterval, cfg.WorkerConcurrency)
	jobHandler := handler.NewJobHandler(jobService)
	executionHandler := handler.NewExecutionHandler(jobService, executionRepo)
	authHandler := handler.NewAuthHandler(userRepo, mailService, entitlementEngine, cfg)
	agentHandler := handler.NewAgentHandler(jobService, cfg)
	pixHandler := handler.NewPixHandler()
	metricsHandler := handler.NewMetricsHandler(cfg.RedisURL)

	// Router
	r := router.New(userRepo, jobHandler, healthHandler, executionHandler, authHandler, agentHandler, pixHandler, metricsHandler, entitlementEngine, cfg.JWTSecret)

	// 5. sobe o servidor
	log.Printf("API rodando em http://localhost:%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatalf("Falha ao iniciar servidor: %v", err)
	}

	queue.NewEnqueuer(cfg.RedisURL)

	
}
