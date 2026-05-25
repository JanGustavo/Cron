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

// Check — GET /health
// @Summary Verificar saúde da API
// @Description Retorna o status de saúde da aplicação e a conectividade com o banco de dados Postgres.
// @Tags Health
// @Produce json
// @Success 200 {object} map[string]string "API operacional e banco conectado"
// @Failure 503 {object} map[string]string "Banco de dados indisponível"
// @Router /health [get]
func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	if err := h.db.Ping(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"status":   "error",
			"postgres": "down",
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "ok",
		"postgres": "up",
	})
}
