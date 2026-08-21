package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/JanGustavo/Cron/internal/auth"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/pkg/httputil"
)

type AlertService struct {
	db          *sql.DB
	mailService *MailService
}

func NewAlertService(db *sql.DB, mailService *MailService) *AlertService {
	return &AlertService{
		db:          db,
		mailService: mailService,
	}
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

func (s *AlertService) Notify(webhookURL, jobID, jobName string, failures, lastStatus int, lastBody string, projectID, jwtSecret string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var req *http.Request
		var err error
		var payloadBytes []byte

		// Se a URL do webhook for do ntfy (ex: ntfy.sh ou contiver ntfy)
		if strings.Contains(webhookURL, "ntfy") {
			message := fmt.Sprintf(
				"Job '%s' falhou %d vezes consecutivas.\nÚltimo Status HTTP: %d\nErro: %s",
				jobName, failures, lastStatus, lastBody,
			)
			payloadBytes = []byte(message)
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
			payloadBytes = data
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
			payloadBytes = data
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
			payloadBytes = data
			req, err = http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(data))
			if err == nil {
				req.Header.Set("Content-Type", "application/json")
			}
		}

		if err != nil {
			log.Printf("AlertService.Notify: erro ao criar request: %v", err)
			return
		}

		// Assinatura de segurança de webhook (Stripe/Svix)
		if projectID != "" && len(payloadBytes) > 0 {
			timestamp := time.Now().Unix()
			
			// Resolve o segredo: prioriza o customizado do banco se existir, cai no fallback de segurança
			secret := ""
			if s.db != nil {
				var dbSecret *string
				errQuery := s.db.QueryRowContext(ctx, `SELECT webhook_secret FROM projects WHERE id = $1`, projectID).Scan(&dbSecret)
				if errQuery == nil && dbSecret != nil && *dbSecret != "" {
					secret = *dbSecret
				}
			}
			if secret == "" {
				secret = auth.ComputeWebhookSecret(projectID, jwtSecret)
			}
			
			sig := auth.SignWebhookPayload(payloadBytes, timestamp, secret)

			req.Header.Set("X-CronFlow-Timestamp", strconv.FormatInt(timestamp, 10))
			req.Header.Set("X-CronFlow-Signature", sig)
		}

		req.Header.Set("User-Agent", "CronFlow-Alerter/1.0")

		resp, err := httputil.SafeClient().Do(req)
		if err != nil {
			log.Printf("AlertService.Notify: falha ao entregar alerta para %s: %v", webhookURL, err)
			return
		}
		defer resp.Body.Close()

		log.Printf("AlertService.Notify: alerta entregue para job %s — status %d", jobID, resp.StatusCode)
	}()
}

// NotifyEmail envia e-mail imediato de alerta de falha de job para usuários Paid/PRO elegíveis.
func (s *AlertService) NotifyEmail(jobID, jobName string, failures, lastStatus int, lastBody string, projectID string) {
	if s.mailService == nil || projectID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var userEmail, plan string
		var emailAlertsEnabled bool
		errUser := s.db.QueryRowContext(ctx, `
			SELECT u.email, u.plan, u.email_alerts_enabled
			FROM users u
			JOIN projects p ON p.user_id = u.id
			WHERE p.id = $1`, projectID).Scan(&userEmail, &plan, &emailAlertsEnabled)

		if errUser == nil && plan == "pro" && emailAlertsEnabled {
			var schedule, url, method string
			errJob := s.db.QueryRowContext(ctx, `SELECT schedule, url, http_method FROM jobs WHERE id = $1`, jobID).Scan(&schedule, &url, &method)

			if errJob == nil {
				var durationMs int
				_ = s.db.QueryRowContext(ctx, `SELECT duration_ms FROM executions WHERE job_id = $1 ORDER BY triggered_at DESC LIMIT 1`, jobID).Scan(&durationMs)

				frontendURL := os.Getenv("FRONTEND_URL")
				if frontendURL == "" {
					frontendURL = "http://localhost:5173"
				}

				errMail := s.mailService.SendFailureAlert(userEmail, frontendURL, jobName, jobID, schedule, url, method, lastBody, failures, lastStatus, durationMs)
				if errMail != nil {
					log.Printf("AlertService.NotifyEmail: erro ao enviar e-mail de alerta: %v", errMail)
				}
			}
		}
	}()
}

// ProcessDailyDigests processa o envio de resumos diários para todos os usuários elegíveis do plano Free.
func (s *AlertService) ProcessDailyDigests(ctx context.Context) {
	if s.mailService == nil {
		return
	}

	userRepo := postgres.NewUserRepository(s.db)
	executionRepo := postgres.NewExecutionRepository(s.db)

	users, err := userRepo.FindUsersEligibleForDigest(ctx)
	if err != nil {
		log.Printf("AlertService.ProcessDailyDigests: erro ao buscar usuários elegíveis: %v", err)
		return
	}

	now := time.Now().UTC()
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:5173"
	}

	for _, u := range users {
		// Carrega o timezone do usuário
		loc, err := time.LoadLocation(u.Timezone)
		if err != nil {
			log.Printf("AlertService.ProcessDailyDigests: timezone inválido '%s' para o usuário %s, caindo de volta para UTC", u.Timezone, u.Email)
			loc = time.UTC
		}

		// Hora local atual do usuário
		nowLocal := now.In(loc)

		// Verifica se já atingiu o horário configurado do resumo (ex: 18h)
		if nowLocal.Hour() >= u.DigestHour {
			// Hoje às digestHour no local timezone do usuário
			digestTimeToday := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), u.DigestHour, 0, 0, 0, loc)

			// Verifica se já enviou hoje
			shouldSend := false
			if u.LastDigestSentAt == nil {
				shouldSend = true
			} else {
				if u.LastDigestSentAt.Before(digestTimeToday) {
					shouldSend = true
				}
			}

			if shouldSend {
				log.Printf("AlertService.ProcessDailyDigests: gerando resumo diário para %s (timezone: %s, hora digest: %d)", u.Email, u.Timezone, u.DigestHour)

				// Busca falhas nas últimas 24h
				failures, err := executionRepo.GetFailedExecutionsForUserLast24Hours(ctx, u.ID)
				if err != nil {
					log.Printf("AlertService.ProcessDailyDigests: erro ao buscar falhas para %s: %v", u.Email, err)
					continue
				}

				// Se houver falhas, envia o e-mail
				if len(failures) > 0 {
					var items []FailedJobDigestItem
					for _, f := range failures {
						items = append(items, FailedJobDigestItem{
							JobID:               f.JobID,
							JobName:             f.JobName,
							Schedule:            f.Schedule,
							URL:                 f.URL,
							HTTPMethod:          f.HTTPMethod,
							ConsecutiveFailures: f.ConsecutiveFailures,
							FailureCount:        f.FailureCount,
							LastHTTPStatus:      f.LastHTTPStatus,
							LastResponseBody:    f.LastResponseBody,
							LastTriggeredAt:     f.LastTriggeredAt,
						})
					}

					errMail := s.mailService.SendDailyDigest(u.Email, frontendURL, items)
					if errMail != nil {
						log.Printf("AlertService.ProcessDailyDigests: erro ao enviar e-mail de resumo para %s: %v", u.Email, errMail)
						continue
					}
				} else {
					log.Printf("AlertService.ProcessDailyDigests: sem falhas registradas para %s nas últimas 24h, pulando envio", u.Email)
				}

				// Atualiza last_digest_sent_at para registrar o envio
				errUpdate := userRepo.UpdateLastDigestSentAt(ctx, u.ID, time.Now())
				if errUpdate != nil {
					log.Printf("AlertService.ProcessDailyDigests: erro ao atualizar last_digest_sent_at para %s: %v", u.Email, errUpdate)
				}
			}
		}
	}
}
