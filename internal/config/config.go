package config

// Config centraliza TODAS as variáveis de ambiente da aplicação.
// Responsabilidade: ler .env / variáveis de sistema e expor uma struct
// tipada para o resto da aplicação. Nenhum outro pacote lê os.Getenv diretamente.

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

// Configurações da aplicação
type Config struct {
	AppEnv      string
	Port        string
	DatabaseURL string
	RedisURL    string
}

// Carrega as variáveis de ambiente e retorna uma instância de Config.
func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("Nenhum .env file encontrado, lendo variáveis de ambiente")
	}

	return &Config{
		AppEnv:      getEnv("APP_ENV", "development"),
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: mustGetEnv("DATABASE_URL"),
		RedisURL:    getEnv("REDIS_URL", "redis://localhost:6379"),
	}
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
