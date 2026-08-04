package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
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
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var req *http.Request
		var err error

		// Se a URL do webhook for do ntfy (ex: ntfy.sh ou contiver ntfy)
		if strings.Contains(webhookURL, "ntfy") {
			message := fmt.Sprintf(
				"Job '%s' falhou %d vezes consecutivas.\nÚltimo Status HTTP: %d\nErro: %s",
				jobName, failures, lastStatus, lastBody,
			)
			req, err = http.NewRequestWithContext(ctx, "POST", webhookURL, strings.NewReader(message))
			if err == nil {
				req.Header.Set("Content-Type", "text/plain")
				req.Header.Set("Title", fmt.Sprintf("CronFlow Alerta: %s", jobName))
				req.Header.Set("Priority", "4") // High priority
				req.Header.Set("Tags", "rotating_light,warning")
			}
		} else if strings.Contains(webhookURL, "discord.com/api/webhooks") || strings.Contains(webhookURL, "discordapp.com/api/webhooks") {
			discordBody := map[string]any{
				"content": fmt.Sprintf(
					"🚨 **CronFlow Alerta**\n**Job:** %s (%s)\n**Falhas Consecutivas:** %d\n**Último Status:** %d\n**Erro:** `%s`",
					jobName, jobID, failures, lastStatus, lastBody,
				),
			}
			data, _ := json.Marshal(discordBody)
			req, err = http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(data))
			if err == nil {
				req.Header.Set("Content-Type", "application/json")
			}
		} else if strings.Contains(webhookURL, "hooks.slack.com") {
			slackBody := map[string]any{
				"text": fmt.Sprintf(
					"🚨 *CronFlow Alerta*\n*Job:* %s (%s)\n*Falhas Consecutivas:* %d\n*Último Status:* %d\n*Erro:* `%s`",
					jobName, jobID, failures, lastStatus, lastBody,
				),
			}
			data, _ := json.Marshal(slackBody)
			req, err = http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(data))
			if err == nil {
				req.Header.Set("Content-Type", "application/json")
			}
		} else {
			// Webhook genérico (JSON)
			payload := alertPayload{
				JobID:       jobID,
				JobName:     jobName,
				Event:       "job.failing",
				Failures:    failures,
				LastStatus:  lastStatus,
				LastError:   lastBody,
				TriggeredAt: time.Now().UTC().Format(time.RFC3339),
			}

			data, errMarshal := json.Marshal(payload)
			if errMarshal != nil {
				log.Printf("AlertService.Notify: erro ao serializar payload: %v", errMarshal)
				return
			}
			req, err = http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(data))
			if err == nil {
				req.Header.Set("Content-Type", "application/json")
			}
		}

		if err != nil {
			log.Printf("AlertService.Notify: erro ao criar request: %v", err)
			return
		}
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
