package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/JanGustavo/Cron/cmd/healthcheck/config"
	"github.com/JanGustavo/Cron/cmd/healthcheck/report"
)

func Send(cfg *config.Config, r *report.Report) {
	if cfg.Notifier.Disabled {
		return
	}

	if cfg.Notifier.Email.Enabled {
		go sendEmail(cfg, r)
	}
	if cfg.Notifier.Slack.Enabled {
		go sendSlack(cfg, r)
	}
	if cfg.Notifier.Webhook.Enabled {
		go sendWebhook(cfg, r)
	}
}

func sendEmail(cfg *config.Config, r *report.Report) {
	if len(cfg.Notifier.Email.To) == 0 || cfg.Notifier.Email.Username == "" {
		return
	}

	subject := fmt.Sprintf("[HealthCheck] %s — %d falhas de %d",
		map[bool]string{true: "🔴 FAIL", false: "🟢 OK"}[r.Failed > 0],
		r.Failed, r.Total)

	body := report.GenerateMarkdown(r)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/markdown; charset=UTF-8\r\n\r\n%s",
		cfg.Notifier.Email.From,
		strings.Join(cfg.Notifier.Email.To, ","),
		subject,
		body)

	auth := smtp.PlainAuth("", cfg.Notifier.Email.Username, cfg.Notifier.Email.Password, cfg.Notifier.Email.SMTPHost)
	addr := fmt.Sprintf("%s:%d", cfg.Notifier.Email.SMTPHost, cfg.Notifier.Email.SMTPPort)

	if err := smtp.SendMail(addr, auth, cfg.Notifier.Email.From, cfg.Notifier.Email.To, []byte(msg)); err != nil {
		fmt.Printf("[Notifier] Erro enviando email: %v\n", err)
	}
}

func sendSlack(cfg *config.Config, r *report.Report) {
	if cfg.Notifier.Slack.WebhookURL == "" {
		return
	}

	color := "#10b981"
	if r.Failed > 0 {
		color = "#ef4444"
	} else if r.Skipped > 0 {
		color = "#f59e0b"
	}

	fields := []map[string]interface{}{
		{"title": "Total", "value": fmt.Sprintf("%d", r.Total), "short": true},
		{"title": "Passed", "value": fmt.Sprintf("%d", r.Passed), "short": true},
		{"title": "Failed", "value": fmt.Sprintf("%d", r.Failed), "short": true},
		{"title": "Skipped", "value": fmt.Sprintf("%d", r.Skipped), "short": true},
		{"title": "Duration", "value": r.Duration.Round(time.Millisecond).String(), "short": true},
		{"title": "Environment", "value": r.Environment, "short": true},
	}

	if r.Diagnostics.FallbackUsed {
		fields = append(fields, map[string]interface{}{
			"title": "⚠️ Fallback", "value": "Local", "short": true,
		})
	}

	payload := map[string]interface{}{
		"channel":  cfg.Notifier.Slack.Channel,
		"username": "CronFlow HealthCheck",
		"icon_emoji": ":stethoscope:",
		"attachments": []map[string]interface{}{
			{
				"color":     color,
				"title":     fmt.Sprintf("Health Check %s", map[bool]string{true: "FAILED", false: "PASSED"}[r.Failed > 0]),
				"title_link": r.BaseURL,
				"fields":    fields,
				"footer":    "CronFlow HealthCheck",
				"ts":        r.Timestamp.Unix(),
			},
		},
	}

	data, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(cfg.Notifier.Slack.WebhookURL, "application/json", bytes.NewReader(data))
	if err != nil {
		fmt.Printf("[Notifier] Erro Slack: %v\n", err)
		return
	}
	resp.Body.Close()
}

func sendWebhook(cfg *config.Config, r *report.Report) {
	if cfg.Notifier.Webhook.URL == "" {
		return
	}

	data, _ := json.Marshal(r)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(cfg.Notifier.Webhook.URL, "application/json", bytes.NewReader(data))
	if err != nil {
		fmt.Printf("[Notifier] Erro webhook: %v\n", err)
		return
	}
	resp.Body.Close()
}