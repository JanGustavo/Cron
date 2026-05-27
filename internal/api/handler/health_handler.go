package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

type HealthHandler struct {
	db *sql.DB
}

func NewHealthHandler(db *sql.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

var (
	Version   = "v1.0.0-dev"
	BuildTime = "unknown"
)

// Check — GET /health
// @Summary Verificar saúde da API
// @Description Retorna o status de saúde da aplicação, conectividade com o banco de dados Postgres e informações de versão do build.
// @Tags Health
// @Produce json
// @Success 200 {object} map[string]string "API operacional e banco conectado"
// @Failure 503 {object} map[string]string "Banco de dados indisponível"
// @Router /health [get]
func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	postgresStatus := "up"
	if err := h.db.Ping(); err != nil {
		postgresStatus = "down"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"status":    "error",
			"postgres":  postgresStatus,
			"version":   Version,
			"buildTime": BuildTime,
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":    "ok",
		"postgres":  postgresStatus,
		"version":   Version,
		"buildTime": BuildTime,
	})
}
