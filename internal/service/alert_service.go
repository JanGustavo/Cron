package service

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type AlertService struct{}

func NewAlertService() *AlertService {
	return &AlertService{}
}

type alertPayload struct {
	JobID       string `json:"job_id"`
	JobName     string `json:"job_name"`
	Event       string `json:"event"`
	Failures    int    `json:"consecutive_failures"`
	LastStatus  int    `json:"last_http_status"`
	LastError   string `json:"last_response_body"`
	TriggeredAt string `json:"triggered_at"`
}

// Notify dispara o webhook de alerta de forma assíncrona.
// Não bloqueia o Worker — roda em goroutine separada.
func (s *AlertService) Notify(webhookURL, jobID, jobName string, failures, lastStatus int, lastBody string) {
	go func() {
		payload := alertPayload{
			JobID:       jobID,
			JobName:     jobName,
			Event:       "job.failing",
			Failures:    failures,
			LastStatus:  lastStatus,
			LastError:   lastBody,
			TriggeredAt: time.Now().UTC().Format(time.RFC3339),
		}

		data, err := json.Marshal(payload)
		if err != nil {
			log.Printf("AlertService.Notify: erro ao serializar payload: %v", err)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		req, _ := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "CronFlow-Alerter/1.0")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("AlertService.Notify: falha ao entregar alerta para %s: %v", webhookURL, err)
			return
		}
		defer resp.Body.Close()

		log.Printf("AlertService.Notify: alerta entregue para job %s — status %d", jobID, resp.StatusCode)
	}()
}
