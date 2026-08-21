package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/JanGustavo/Cron/internal/config"
	"github.com/redis/go-redis/v9"
)

type HealthHandler struct {
	db                *sql.DB
	redisURL          string
	appEnv            string
	schedulerInterval string
	workerConcurrency int
	cfg               *config.Config
}

func NewHealthHandler(db *sql.DB, redisURL string, appEnv, schedulerInterval string, workerConcurrency int, cfg *config.Config) *HealthHandler {
	return &HealthHandler{
		db:                db,
		redisURL:          redisURL,
		appEnv:            appEnv,
		schedulerInterval: schedulerInterval,
		workerConcurrency: workerConcurrency,
		cfg:               cfg,
	}
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
	var opt *redis.Options
	var err error
	if len(h.redisURL) >= 8 && h.redisURL[:8] == "redis://" {
		opt, err = redis.ParseURL(h.redisURL)
		if err != nil {
			redisStatus = "down"
		}
	} else {
		opt = &redis.Options{
			Addr: h.redisURL,
		}
	}

	if redisStatus == "up" {
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
	json.NewEncoder(w).Encode(map[string]any{
		"status":    status,
		"postgres":  postgresStatus,
		"redis":     redisStatus,
		"version":   Version,
		"buildTime": BuildTime,
		"config": map[string]any{
			"appEnv":            h.appEnv,
			"schedulerInterval": h.schedulerInterval,
			"workerConcurrency": h.workerConcurrency,
		},
	})
}

// CheckAI — GET /v1/health/ai
// @Summary Verificar disponibilidade dos modelos de IA configurados (Gemini / Groq)
// @Description Realiza chamadas de diagnóstico leves para verificar se os tokens e modelos configurados do Gemini e Groq estão operacionais.
// @Tags Health
// @Produce json
// @Success 200 {object} map[string]string "Modelos de IA disponíveis"
// @Failure 502 {object} map[string]string "Serviço de IA indisponível ou configurado incorretamente"
// @Router /v1/health/ai [get]
func (h *HealthHandler) CheckAI(w http.ResponseWriter, r *http.Request) {
	geminiStatus := "disabled"
	geminiErr := ""

	if h.cfg.GeminiAPIKey != "" && !h.cfg.DisableGemini {
		geminiStatus = "up"
		client := &http.Client{Timeout: 8 * time.Second}
		geminiUrl := "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.5-flash-lite?key=" + h.cfg.GeminiAPIKey

		req, err := http.NewRequestWithContext(r.Context(), "GET", geminiUrl, nil)
		if err != nil {
			geminiStatus = "error"
			geminiErr = err.Error()
		} else {
			resp, err := client.Do(req)
			if err != nil {
				geminiStatus = "down"
				geminiErr = err.Error()
			} else {
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					geminiStatus = "down"
					var errBody map[string]any
					_ = json.NewDecoder(resp.Body).Decode(&errBody)
					errBytes, _ := json.Marshal(errBody)
					geminiErr = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(errBytes))
				}
			}
		}
	}

	groqStatus := "disabled"
	groqErr := ""

	if h.cfg.GroqAPIKey != "" {
		groqStatus = "up"
		client := &http.Client{Timeout: 8 * time.Second}
		groqUrl := "https://api.groq.com/openai/v1/models"

		req, err := http.NewRequestWithContext(r.Context(), "GET", groqUrl, nil)
		if err != nil {
			groqStatus = "error"
			groqErr = err.Error()
		} else {
			req.Header.Set("Authorization", "Bearer "+h.cfg.GroqAPIKey)
			resp, err := client.Do(req)
			if err != nil {
				groqStatus = "down"
				groqErr = err.Error()
			} else {
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					groqStatus = "down"
					var errBody map[string]any
					_ = json.NewDecoder(resp.Body).Decode(&errBody)
					errBytes, _ := json.Marshal(errBody)
					groqErr = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(errBytes))
				} else {
					var modelsResp struct {
						Data []struct {
							ID string `json:"id"`
						} `json:"data"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
						groqStatus = "error"
						groqErr = "Failed to decode models response: " + err.Error()
					} else {
						found := false
						var availableModels []string
						for _, m := range modelsResp.Data {
							availableModels = append(availableModels, m.ID)
							if m.ID == "openai/gpt-oss-20b" {
								found = true
							}
						}
						if !found {
							groqStatus = "down"
							groqErr = fmt.Sprintf("modelo 'openai/gpt-oss-20b' nao encontrado na lista de modelos disponiveis. Modelos disponiveis: %v", availableModels)
						}
					}
				}
			}
		}
	}

	status := "ok"
	statusCode := http.StatusOK
	if (geminiStatus == "down" || geminiStatus == "error" || geminiStatus == "disabled") &&
		(groqStatus == "down" || groqStatus == "error" || groqStatus == "disabled") {
		status = "error"
		statusCode = http.StatusBadGateway
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]any{
		"status": status,
		"gemini": map[string]any{
			"status": geminiStatus,
			"error":  geminiErr,
		},
		"groq": map[string]any{
			"status": groqStatus,
			"error":  groqErr,
		},
	})
}
