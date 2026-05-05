package config

// Config centraliza TODAS as variáveis de ambiente da aplicação.
// Responsabilidade: ler .env / variáveis de sistema e expor uma struct
// tipada para o resto da aplicação. Nenhum outro pacote lê os.Getenv diretamente.

type Config struct {
	DatabaseURL string
	RedisURL    string
	Port        string
	Env         string
}
