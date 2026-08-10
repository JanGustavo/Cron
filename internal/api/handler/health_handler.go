package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/redis/go-redis/v9"
)

type HealthHandler struct {
	db       *sql.DB
	redisURL string
}

func NewHealthHandler(db *sql.DB, redisURL string) *HealthHandler {
	return &HealthHandler{db: db, redisURL: redisURL}
}

var (
	Version   = "v1.0.0-dev"
	BuildTime = "unknown"
)

// Check — GET /health
// @Summary Verificar saúde da API
// @Description Retorna o status de saúde da aplicação, conectividade com o banco de dados Postgres, cache Redis e informações de versão do build.
// @Tags Health
// @Produce json
// @Success 200 {object} map[string]string "API operacional e banco conectado"
// @Failure 503 {object} map[string]string "Banco de dados ou Redis indisponível"
// @Router /health [get]
func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	postgresStatus := "up"
	if err := h.db.Ping(); err != nil {
		postgresStatus = "down"
	}

	redisStatus := "up"
	opt, err := redis.ParseURL(h.redisURL)
	if err != nil {
		redisStatus = "down"
	} else {
		rdb := redis.NewClient(opt)
		if err := rdb.Ping(r.Context()).Err(); err != nil {
			redisStatus = "down"
		}
		rdb.Close()
	}

	status := "ok"
	statusCode := http.StatusOK
	if postgresStatus == "down" || redisStatus == "down" {
		status = "error"
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{
		"status":    status,
		"postgres":  postgresStatus,
		"redis":     redisStatus,
		"version":   Version,
		"buildTime": BuildTime,
	})
}
