package handler

import (
	"net/http"
	"runtime"

	"github.com/hibiken/asynq"
)

type MetricsHandler struct {
	redisAddr string
}

func NewMetricsHandler(redisAddr string) *MetricsHandler {
	return &MetricsHandler{redisAddr: redisAddr}
}

type SystemStats struct {
	GoroutinesCount int    `json:"goroutines_count"`
	AllocBytes      uint64 `json:"alloc_bytes"`
	SysBytes        uint64 `json:"sys_bytes"`
	HeapSysBytes    uint64 `json:"heap_sys_bytes"`
	HeapObjects     uint64 `json:"heap_objects"`
	NumGC           uint32 `json:"num_gc"`
}

// SystemMetrics — GET /v1/metrics/system
func (h *MetricsHandler) SystemMetrics(w http.ResponseWriter, r *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	stats := SystemStats{
		GoroutinesCount: runtime.NumGoroutine(),
		AllocBytes:      m.Alloc,
		SysBytes:        m.Sys,
		HeapSysBytes:    m.HeapSys,
		HeapObjects:     m.HeapObjects,
		NumGC:           m.NumGC,
	}

	writeJSON(w, http.StatusOK, stats)
}

type QueueStats struct {
	Name      string `json:"name"`
	Size      int    `json:"size"`
	Pending   int    `json:"pending"`
	Active    int    `json:"active"`
	Scheduled int    `json:"scheduled"`
	Retry     int    `json:"retry"`
	Archived  int    `json:"archived"`
	Paused    bool   `json:"paused"`
}

// QueueMetrics — GET /v1/metrics/queue
// @Summary Obter métricas das filas do Redis
// @Description Retorna estatísticas detalhadas de tamanho, tarefas ativas, agendadas e pendentes da fila do Asynq.
// @Tags Métricas
// @Produce json
// @Success 200 {object} map[string]interface{} "Estatísticas das filas"
// @Failure 401 {object} map[string]string "Não autenticado"
// @Failure 500 {object} map[string]string "Erro interno"
// @Security ApiKeyAuth
// @Router /v1/metrics/queue [get]
func (h *MetricsHandler) QueueMetrics(w http.ResponseWriter, r *http.Request) {
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: h.redisAddr})
	defer inspector.Close()

	queues, err := inspector.Queues()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao listar filas do Redis")
		return
	}

	var stats []QueueStats
	for _, qName := range queues {
		info, err := inspector.GetQueueInfo(qName)
		if err != nil {
			continue
		}
		stats = append(stats, QueueStats{
			Name:      info.Queue,
			Size:      info.Size,
			Pending:   info.Pending,
			Active:    info.Active,
			Scheduled: info.Scheduled,
			Retry:     info.Retry,
			Archived:  info.Archived,
			Paused:    info.Paused,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"queues": stats,
	})
}
