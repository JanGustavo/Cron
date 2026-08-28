package config

// Config centraliza TODAS as variáveis de ambiente da aplicação.
// Responsabilidade: ler .env / variáveis de sistema e expor uma struct
// tipada para o resto da aplicação. Nenhum outro pacote lê os.Getenv diretamente.

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Configurações da aplicação
type Config struct {
	AppEnv             string
	Port               string
	DatabaseURL        string
	RedisURL           string
	MaxJobsFreePlan    int
	MaxJobsPaidPlan    int
	JWTSecret          string
	GeminiAPIKey       string
	GroqAPIKey         string
	GoogleClientID     string
	GoogleClientSecret string
	GitHubClientID     string
	GitHubClientSecret string
	APIURL             string
	FrontendURL        string
	SmtpHost           string
	SmtpPort           int
	SmtpUser           string
	SmtpPass           string
	SmtpFrom           string
	SchedulerInterval  string
	WorkerConcurrency  int
	StripePublishableKey string
	StripeSecretKey      string
	StripeWebhookSecret  string
	StripePriceIDProMonthly string
	StripePriceIDProYearly  string
	DisableGemini          bool
	BillingProvider        string
	AsaasAPIKey            string
	AsaasWebhookToken      string
}

// Carrega as variáveis de ambiente e retorna uma instância de Config.
func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("Nenhum .env file encontrado, lendo variáveis de ambiente")
	}

	c := &Config{
		AppEnv:             getEnv("APP_ENV", "development"), //env / fallback
		Port:               getEnv("PORT", "8080"),
		DatabaseURL:        mustGetEnv("DATABASE_URL"),
		RedisURL:           getEnv("REDIS_URL", "redis://localhost:6379"),
		MaxJobsFreePlan:    getEnvAsInt("MAX_JOBS_FREE_PLAN", 3),
		MaxJobsPaidPlan:    getEnvAsInt("MAX_JOBS_PAID_PLAN", 20),
		JWTSecret:          getEnv("JWT_SECRET", "cronflow_jwt_secret_fallback_key_2026_xyz"),
		GeminiAPIKey:       getEnv("GEMINI_API_KEY", ""),
		GroqAPIKey:         getEnv("GROQ_API_KEY", ""),
		GoogleClientID:     getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: getEnv("GOOGLE_CLIENT_SECRET", ""),
		GitHubClientID:     getEnv("GITHUB_CLIENT_ID", ""),
		GitHubClientSecret: getEnv("GITHUB_CLIENT_SECRET", ""),
		APIURL:             getEnv("API_URL", "http://localhost:8080"),
		FrontendURL:        getEnv("FRONTEND_URL", "http://localhost:5173"),
		SmtpHost:           getEnv("SMTP_HOST", ""),
		SmtpPort:           getEnvAsInt("SMTP_PORT", 587),
		SmtpUser:           getEnv("SMTP_USER", ""),
		SmtpPass:           getEnv("SMTP_PASS", ""),
		SmtpFrom:           getEnv("SMTP_FROM", "no-reply@cronflow.me"),
		SchedulerInterval:  getEnv("SCHEDULER_INTERVAL", "30s"),
		WorkerConcurrency:  getEnvAsInt("WORKER_CONCURRENCY", 50),
		StripePublishableKey: getEnv("STRIPE_PUBLISHABLE_KEY", ""),
		StripeSecretKey:      getEnv("STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret:  getEnv("STRIPE_WEBHOOK_SECRET", ""),
		StripePriceIDProMonthly: getEnv("STRIPE_PRICE_ID_PRO_MONTHLY", ""),
		StripePriceIDProYearly:  getEnv("STRIPE_PRICE_ID_PRO_YEARLY", ""),
		DisableGemini:          getEnvAsBool("DISABLE_GEMINI", false),
		BillingProvider:        getEnv("BILLING_PROVIDER", "stripe"),
		AsaasAPIKey:            getEnv("ASAAS_API_KEY", ""),
		AsaasWebhookToken:      getEnv("ASAAS_WEBHOOK_TOKEN", ""),
	}

	// -------------------------------------------------------------
	// 🔒 VALIDAÇÕES DE STARTUP (ESTABILIZAÇÃO & SEGURANÇA)
	// -------------------------------------------------------------

	// 1. Previne JWT_SECRET fraco ou de fallback em produção (P0)
	if c.AppEnv == "production" {
		if c.JWTSecret == "cronflow_jwt_secret_fallback_key_2026_xyz" || len(c.JWTSecret) < 32 {
			log.Fatalf("FATAL: JWT_SECRET não está configurado de forma segura em ambiente de produção (deve ter pelo menos 32 caracteres).")
		}
	}

	// 2. Valida limites para SCHEDULER_INTERVAL (deve ser entre 1s e 1h) (P1)
	if c.SchedulerInterval != "" {
		if d, err := time.ParseDuration(c.SchedulerInterval); err == nil {
			if d < 1*time.Second || d > 1*time.Hour {
				log.Printf("WARNING: SCHEDULER_INTERVAL=%s fora da faixa de segurança (1s a 1h). Usando fallback=30s.", c.SchedulerInterval)
				c.SchedulerInterval = "30s"
			}
		} else {
			log.Printf("WARNING: SCHEDULER_INTERVAL=%s formato inválido. Usando fallback=30s.", c.SchedulerInterval)
			c.SchedulerInterval = "30s"
		}
	} else {
		c.SchedulerInterval = "30s"
	}

	// 3. Valida limites para WORKER_CONCURRENCY (deve ser entre 1 e 500) (P1)
	if c.WorkerConcurrency < 1 || c.WorkerConcurrency > 500 {
		log.Printf("WARNING: WORKER_CONCURRENCY=%d fora da faixa de segurança (1 a 500). Usando fallback=50.", c.WorkerConcurrency)
		c.WorkerConcurrency = 50
	}

	return c
}

// getEnv retorna o valor da variável de ambiente ou um fallback se não estiver definida.
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	log.Printf("INFO: variável de ambiente %s não definida, usando fallback: %s", key, fallback)
	return fallback
}

// mustGetEnv retorna o valor da variável de ambiente ou loga um erro fatal se não estiver definida.
func mustGetEnv(key string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	log.Fatalf("FATAL: variável de ambiente obrigatória não definida %s", key)
	return ""
}

// getEnvAsInt lê uma variável de ambiente e converte para int, usando o fallback em caso de falha ou ausência
func getEnvAsInt(key string, fallback int) int {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return fallback
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		log.Printf("ERRO: variável de ambiente %s=%s inválida para int, usando fallback: %d", key, valueStr, fallback)
		return fallback
	}
	return value
}

// getEnvAsBool lê uma variável de ambiente e converte para bool, usando o fallback em caso de falha ou ausência
func getEnvAsBool(key string, fallback bool) bool {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return fallback
	}
	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		log.Printf("ERRO: variável de ambiente %s=%s inválida para bool, usando fallback: %t", key, valueStr, fallback)
		return fallback
	}
	return value
}

