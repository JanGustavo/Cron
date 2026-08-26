package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
)

type MailService struct {
	host      string
	port      int
	user      string
	pass      string
	from      string
	resendKey string
}

func NewMailService(host string, port int, user, pass, from string) *MailService {
	return &MailService{
		host:      host,
		port:      port,
		user:      user,
		pass:      pass,
		from:      from,
		resendKey: os.Getenv("RESEND_API_KEY"),
	}
}

func (s *MailService) sendViaResend(to, subject, body string) error {
	url := "https://api.resend.com/emails"

	payload := map[string]interface{}{
		"from":    s.from,
		"to":      []string{to},
		"subject": subject,
		"html":    body,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal resend payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.resendKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var resErr map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&resErr)
		return fmt.Errorf("resend api returned error status %d: %v", resp.StatusCode, resErr)
	}

	return nil
}

// SendPasswordResetEmail envia o link de redefinição de senha para o e-mail do usuário.
// Se as configurações de SMTP não estiverem preenchidas, ele apenas simula o envio imprimindo no console.
func (s *MailService) SendPasswordResetEmail(to, resetLink string) error {
	subject := "Recuperação de Senha - CronFlow"
	
	// Template HTML elegante e premium
	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; background-color: #0b0f19; color: #f8fafc; margin: 0; padding: 40px 20px; }
        .card { max-width: 500px; margin: 0 auto; background: #111827; border: 1px solid #1e293b; border-radius: 16px; padding: 32px; box-shadow: 0 10px 15px -3px rgba(0, 0, 0, 0.3); }
        .logo { font-size: 20px; font-weight: bold; color: #6366f1; text-align: center; margin-bottom: 24px; }
        h2 { font-size: 18px; margin-top: 0; color: #f1f5f9; text-align: center; }
        p { font-size: 14px; line-height: 1.6; color: #94a3b8; }
        .btn-container { text-align: center; margin: 32px 0; }
        .btn { display: inline-block; padding: 12px 24px; font-size: 14px; font-weight: bold; color: #ffffff !important; background-color: #4f46e5; border-radius: 12px; text-decoration: none; transition: background-color 0.2s; }
        .btn:hover { background-color: #4338ca; }
        .footer { font-size: 11px; color: #475569; text-align: center; margin-top: 32px; line-height: 1.4; }
        .fallback-link { word-break: break-all; font-size: 12px; color: #6366f1; text-decoration: none; }
    </style>
</head>
<body>
    <div class="card">
        <div class="logo">CronFlow</div>
        <h2>Recuperação de Senha</h2>
        <p>Olá,</p>
        <p>Recebemos uma solicitação para redefinir a senha da sua conta no CronFlow. Clique no botão abaixo para escolher uma nova senha. Este link expira em 15 minutos.</p>
        <div class="btn-container">
            <a href="%s" class="btn" target="_blank">Redefinir Senha</a>
        </div>
        <p>Se o botão acima não funcionar, copie e cole o seguinte link no seu navegador:</p>
        <p><a href="%s" class="fallback-link" target="_blank">%s</a></p>
        <hr style="border: 0; border-top: 1px solid #1e293b; margin: 24px 0;">
        <p class="footer">Se você não solicitou esta alteração, por favor ignore este e-mail.<br>&copy; 2026 CronFlow. Todos os direitos reservados.</p>
    </div>
</body>
</html>`, resetLink, resetLink, resetLink)

	if s.resendKey != "" {
		err := s.sendViaResend(to, subject, body)
		if err != nil {
			return fmt.Errorf("MailService.SendPasswordResetEmail (Resend): %w", err)
		}
		log.Printf("MailService: E-mail de redefinição de senha enviado com sucesso via Resend para %s", to)
		return nil
	}

	if s.host == "" || s.user == "" {
		log.Printf("\n========================================================================\n" +
			"[MOCK EMAIL] Envio de Recuperação de Senha (Sem SMTP configurado)\n" +
			"Para: %s\n" +
			"Assunto: %s\n" +
			"Link: %s\n" +
			"========================================================================\n", to, subject, resetLink)
		return nil
	}

	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	auth := smtp.PlainAuth("", s.user, s.pass, s.host)
	msg := []byte(fmt.Sprintf("To: %s\r\n"+
		"Subject: %s\r\n"+
		"MIME-version: 1.0;\r\n"+
		"Content-Type: text/html; charset=\"UTF-8\";\r\n\r\n"+
		"%s\r\n", to, subject, body))

	err := smtp.SendMail(addr, auth, s.from, []string{to}, msg)
	if err != nil {
		return fmt.Errorf("MailService.SendPasswordResetEmail: %w", err)
	}

	log.Printf("MailService: E-mail de redefinição de senha enviado com sucesso para %s", to)
	return nil
}

// SendVerificationEmail envia o link de confirmação de conta para o e-mail do usuário.
func (s *MailService) SendVerificationEmail(to, verificationLink string) error {
	subject := "Confirme seu E-mail - CronFlow"
	
	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; background-color: #0b0f19; color: #f8fafc; margin: 0; padding: 40px 20px; }
        .card { max-width: 500px; margin: 0 auto; background: #111827; border: 1px solid #1e293b; border-radius: 16px; padding: 32px; box-shadow: 0 10px 15px -3px rgba(0, 0, 0, 0.3); }
        .logo { font-size: 20px; font-weight: bold; color: #6366f1; text-align: center; margin-bottom: 24px; }
        h2 { font-size: 18px; margin-top: 0; color: #f1f5f9; text-align: center; }
        p { font-size: 14px; line-height: 1.6; color: #94a3b8; }
        .btn-container { text-align: center; margin: 32px 0; }
        .btn { display: inline-block; padding: 12px 24px; font-size: 14px; font-weight: bold; color: #ffffff !important; background-color: #4f46e5; border-radius: 12px; text-decoration: none; transition: background-color 0.2s; }
        .btn:hover { background-color: #4338ca; }
        .footer { font-size: 11px; color: #475569; text-align: center; margin-top: 32px; line-height: 1.4; }
        .fallback-link { word-break: break-all; font-size: 12px; color: #6366f1; text-decoration: none; }
    </style>
</head>
<body>
    <div class="card">
        <div class="logo">CronFlow</div>
        <h2>Confirmação de Cadastro</h2>
        <p>Olá,</p>
        <p>Agradecemos por se cadastrar no CronFlow! Clique no botão abaixo para confirmar seu endereço de e-mail e ativar a sua conta.</p>
        <div class="btn-container">
            <a href="%s" class="btn" target="_blank">Confirmar E-mail</a>
        </div>
        <p>Se o botão acima não funcionar, copie e cole o seguinte link no seu navegador:</p>
        <p><a href="%s" class="fallback-link" target="_blank">%s</a></p>
        <hr style="border: 0; border-top: 1px solid #1e293b; margin: 24px 0;">
        <p class="footer">Se você não realizou este cadastro, por favor ignore este e-mail.<br>&copy; 2026 CronFlow. Todos os direitos reservados.</p>
    </div>
</body>
</html>`, verificationLink, verificationLink, verificationLink)

	if s.resendKey != "" {
		err := s.sendViaResend(to, subject, body)
		if err != nil {
			return fmt.Errorf("MailService.SendVerificationEmail (Resend): %w", err)
		}
		log.Printf("MailService: E-mail de confirmação enviado com sucesso via Resend para %s", to)
		return nil
	}

	if s.host == "" || s.user == "" {
		log.Printf("\n========================================================================\n" +
			"[MOCK EMAIL] Envio de Confirmação de E-mail (Sem SMTP configurado)\n" +
			"Para: %s\n" +
			"Assunto: %s\n" +
			"Link: %s\n" +
			"========================================================================\n", to, subject, verificationLink)
		return nil
	}

	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	auth := smtp.PlainAuth("", s.user, s.pass, s.host)
	msg := []byte(fmt.Sprintf("To: %s\r\n"+
		"Subject: %s\r\n"+
		"MIME-version: 1.0;\r\n"+
		"Content-Type: text/html; charset=\"UTF-8\";\r\n\r\n"+
		"%s\r\n", to, subject, body))

	err := smtp.SendMail(addr, auth, s.from, []string{to}, msg)
	if err != nil {
		return fmt.Errorf("MailService.SendVerificationEmail: %w", err)
	}

	log.Printf("MailService: E-mail de confirmação enviado com sucesso para %s", to)
	return nil
}

// SendFailureAlert envia um alerta de falha de execução de job.
func (s *MailService) SendFailureAlert(to, frontendURL string, jName, jID, schedule, url, method, errorMsg string, consecutiveFailures, httpStatus int, durationMs int) error {
	subject := fmt.Sprintf("🚨 Falha Crítica: Job '%s' Suspenso - CronFlow", jName)

	// Formata a especificação do Job como JSON bonitinho (Prettier-like)
	jobSpec := map[string]any{
		"id":          jID,
		"name":        jName,
		"schedule":    schedule,
		"url":         url,
		"http_method": method,
	}
	jobJSONBytes, _ := json.MarshalIndent(jobSpec, "", "  ")
	jobJSON := string(jobJSONBytes)

	statusStr := "ERR"
	if httpStatus > 0 {
		statusStr = fmt.Sprintf("%d", httpStatus)
	}

	btnLink := fmt.Sprintf("%s/?jobId=%s", frontendURL, jID)

	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; background-color: #05070f; color: #f8fafc; margin: 0; padding: 40px 20px; }
        .card { max-width: 600px; margin: 0 auto; background: #0b0f19; border: 1px solid #1e293b; border-radius: 24px; padding: 36px; box-shadow: 0 20px 25px -5px rgba(0, 0, 0, 0.4); }
        .logo { font-size: 16px; font-weight: 800; color: #818cf8; text-transform: uppercase; letter-spacing: 0.15em; margin-bottom: 28px; }
        .alert-header { display: inline-flex; align-items: center; gap: 8px; padding: 6px 14px; background: rgba(239, 68, 68, 0.1); border: 1px solid rgba(239, 68, 68, 0.2); border-radius: 9999px; font-size: 11px; font-weight: 800; text-transform: uppercase; letter-spacing: 0.05em; color: #f87171; margin-bottom: 16px; }
        h2 { font-size: 22px; font-weight: 900; margin-top: 0; color: #f1f5f9; line-height: 1.3; }
        p { font-size: 14px; line-height: 1.6; color: #94a3b8; margin: 0 0 16px 0; }
        
        .details-table { border-collapse: collapse; margin: 24px 0; }
        .details-table td { padding: 12px; background: #111827; border: 1px solid #1e293b; border-radius: 12px; text-align: left; }
        .detail-label { font-size: 9px; font-weight: bold; text-transform: uppercase; color: #64748b; display: block; letter-spacing: 0.05em; }
        .detail-val { font-size: 12px; font-weight: bold; color: #e2e8f0; display: block; margin-top: 4px; }
        
        .error-panel { padding: 16px; background: rgba(239, 68, 68, 0.05); border: 1px solid rgba(239, 68, 68, 0.15); border-radius: 16px; margin: 24px 0; }
        .error-title { font-size: 11px; font-weight: bold; text-transform: uppercase; color: #f87171; margin-bottom: 6px; display: block; }
        .error-body { font-family: monospace; font-size: 12px; color: #fca5a5; word-break: break-all; margin: 0; white-space: pre-wrap; }
        .code-container { margin: 24px 0; }
        .code-title { font-size: 10px; font-weight: bold; text-transform: uppercase; color: #64748b; margin-bottom: 8px; display: block; }
        .code-block { padding: 16px; background: #030712; border: 1px solid #111827; border-radius: 16px; font-family: monospace; font-size: 11.5px; color: #a5b4fc; overflow-x: auto; margin: 0; white-space: pre-wrap; word-break: break-all; }
        .btn-container { text-align: center; margin: 32px 0 16px 0; }
        .btn { display: inline-block; padding: 14px 28px; font-size: 13px; font-weight: bold; color: #ffffff !important; background: #4f46e5; border-radius: 14px; text-decoration: none; box-shadow: 0 4px 14px rgba(79, 70, 229, 0.3); transition: all 0.2s; }
        .btn:hover { background: #4338ca; box-shadow: 0 6px 20px rgba(79, 70, 229, 0.4); }
        .footer { font-size: 11px; color: #475569; text-align: center; margin-top: 36px; border-top: 1px solid #1e293b; padding-top: 24px; line-height: 1.5; }
    </style>
</head>
<body>
    <div class="card">
        <div class="logo">CronFlow</div>
        <div class="alert-header">🚨 FALHA DE DISPARO</div>
        <h2>A tarefa '%s' foi suspensa após falhar consecutivamente</h2>
        <p>Olá,</p>
        <p>O processador do CronFlow tentou efetuar a execução da tarefa abaixo, porém o endpoint falhou. Devido ao limite de 3 falhas seguidas ser atingido, a execução foi suspensa e o job foi marcado como **Failing**.</p>
        
        <table class="details-table" width="100%%" cellpadding="0" cellspacing="0">
            <tr>
                <td width="48%%">
                    <span class="detail-label">Status do Erro</span>
                    <span class="detail-val" style="color: #f87171;">%s FAILED</span>
                </td>
                <td width="4%%" style="background: transparent; border: none;"></td>
                <td width="48%%">
                    <span class="detail-label">Duração</span>
                    <span class="detail-val">%dms</span>
                </td>
            </tr>
        </table>

        <div class="error-panel">
            <span class="error-title">Detalhe da Falha</span>
            <pre class="error-body">%s</pre>
        </div>

        <div class="code-container">
            <span class="code-title">Definição do Job (JSON)</span>
            <pre class="code-block">%s</pre>
        </div>

        <div class="btn-container">
            <a href="%s" class="btn" target="_blank">Visualizar Job no Painel</a>
        </div>

        <p class="footer">Este é um e-mail automático enviado pelo CronFlow Alertas.<br>Para desativar estes avisos, acesse as Configurações de Perfil no painel.<br>&copy; 2026 CronFlow. Todos os direitos reservados.</p>
    </div>
</body>
</html>`, jName, statusStr, durationMs, errorMsg, jobJSON, btnLink)

	if s.resendKey != "" {
		err := s.sendViaResend(to, subject, body)
		if err != nil {
			return fmt.Errorf("MailService.SendFailureAlert (Resend): %w", err)
		}
		log.Printf("MailService: E-mail de alerta de falha enviado com sucesso via Resend para %s", to)
		return nil
	}

	if s.host == "" || s.user == "" {
		log.Printf("\n========================================================================\n" +
			"[MOCK EMAIL] Envio de Alerta de Falha (Sem SMTP configurado)\n" +
			"Para: %s\n" +
			"Assunto: %s\n" +
			"Job: %s (ID: %s)\n" +
			"Erro: %s\n" +
			"========================================================================\n", to, subject, jName, jID, errorMsg)
		return nil
	}

	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	auth := smtp.PlainAuth("", s.user, s.pass, s.host)
	msg := []byte(fmt.Sprintf("To: %s\r\n"+
		"Subject: %s\r\n"+
		"MIME-version: 1.0;\r\n"+
		"Content-Type: text/html; charset=\"UTF-8\";\r\n\r\n"+
		"%s\r\n", to, subject, body))

	err := smtp.SendMail(addr, auth, s.from, []string{to}, msg)
	if err != nil {
		return fmt.Errorf("MailService.SendFailureAlert: %w", err)
	}

	log.Printf("MailService: E-mail de alerta de falha enviado com sucesso para %s", to)
	return nil
}

type FailedJobDigestItem struct {
	JobID               string
	JobName             string
	Schedule            string
	URL                 string
	HTTPMethod          string
	ConsecutiveFailures int
	FailureCount        int
	LastHTTPStatus      int
	LastResponseBody    string
	LastTriggeredAt     string
}

// SendDailyDigest envia um resumo diário consolidado das falhas de execução.
func (s *MailService) SendDailyDigest(to, frontendURL string, items []FailedJobDigestItem) error {
	subject := "🚨 Resumo Diário de Falhas - CronFlow"

	var cardsHTML string
	for _, item := range items {
		jobSpec := map[string]any{
			"id":          item.JobID,
			"name":        item.JobName,
			"schedule":    item.Schedule,
			"url":         item.URL,
			"http_method": item.HTTPMethod,
		}
		jobJSONBytes, _ := json.MarshalIndent(jobSpec, "", "  ")
		jobJSON := string(jobJSONBytes)

		statusStr := "ERR"
		if item.LastHTTPStatus > 0 {
			statusStr = fmt.Sprintf("%d", item.LastHTTPStatus)
		}

		statusBadgeColor := "#f87171"
		statusBadgeText := "FAILED"
		if item.ConsecutiveFailures >= 4 {
			statusBadgeText = "SUSPENSO (Pausa Forçada)"
			statusBadgeColor = "#ef4444"
		}

		btnLink := fmt.Sprintf("%s/?jobId=%s", frontendURL, item.JobID)

		cardsHTML += fmt.Sprintf(`
        <div class="job-card" style="background: #111827; border: 1px solid #1e293b; border-radius: 16px; padding: 24px; margin-bottom: 20px; text-align: left;">
            <div class="job-header" style="margin-bottom: 6px;">
                <span class="job-method" style="display: inline-block; padding: 3px 8px; background: #1f2937; border-radius: 6px; font-size: 11px; font-weight: bold; color: #a5b4fc; margin-right: 8px; font-family: monospace;">%s</span>
                <span class="job-name" style="font-size: 15px; font-weight: bold; color: #f1f5f9;">%s</span>
            </div>
            <div class="job-url" style="font-family: monospace; font-size: 12px; color: #64748b; margin-bottom: 16px; word-break: break-all;">%s</div>
            
            <table class="stats-table" width="100%%" cellpadding="0" cellspacing="0" style="border-collapse: collapse; margin-bottom: 16px;">
                <tr>
                    <td width="30%%" style="padding: 10px; background: #0b0f19; border: 1px solid #1e293b; border-radius: 8px; text-align: left;">
                        <span class="stat-label" style="font-size: 9px; font-weight: bold; text-transform: uppercase; color: #64748b; display: block;">Falhas Hoje</span>
                        <span class="stat-value" style="font-size: 12px; font-weight: bold; color: #e2e8f0; display: block; margin-top: 4px;">%d</span>
                    </td>
                    <td width="5%%" style="background: transparent; border: none;"></td>
                    <td width="30%%" style="padding: 10px; background: #0b0f19; border: 1px solid #1e293b; border-radius: 8px; text-align: left;">
                        <span class="stat-label" style="font-size: 9px; font-weight: bold; text-transform: uppercase; color: #64748b; display: block;">Último Status</span>
                        <span class="stat-value" style="font-size: 12px; font-weight: bold; color: %s; display: block; margin-top: 4px;">%s</span>
                    </td>
                    <td width="5%%" style="background: transparent; border: none;"></td>
                    <td width="30%%" style="padding: 10px; background: #0b0f19; border: 1px solid #1e293b; border-radius: 8px; text-align: left;">
                        <span class="stat-label" style="font-size: 9px; font-weight: bold; text-transform: uppercase; color: #64748b; display: block;">Estado Atual</span>
                        <span class="stat-value" style="font-size: 11px; font-weight: bold; color: %s; display: block; margin-top: 4px;">%s</span>
                    </td>
                </tr>
            </table>

            <div class="panel-section" style="padding: 12px; background: rgba(239, 68, 68, 0.02); border: 1px solid rgba(239, 68, 68, 0.08); border-radius: 12px; margin-top: 12px;">
                <span class="section-title" style="font-size: 9px; font-weight: bold; text-transform: uppercase; color: #f87171; display: block; margin-bottom: 6px;">Último Retorno de Erro</span>
                <pre class="error-text" style="font-family: monospace; font-size: 11px; color: #fca5a5; margin: 0; white-space: pre-wrap; word-break: break-all;">%s</pre>
            </div>

            <div class="panel-section" style="padding: 12px; background: #030712; border: 1px solid #1f2937; border-radius: 12px; margin-top: 12px;">
                <span class="section-title" style="font-size: 9px; font-weight: bold; text-transform: uppercase; color: #818cf8; display: block; margin-bottom: 6px;">Definição do Job (JSON)</span>
                <pre class="json-text" style="font-family: monospace; font-size: 11px; color: #a5b4fc; margin: 0; white-space: pre-wrap; word-break: break-all;">%s</pre>
            </div>

            <div style="text-align: right; margin-top: 16px;">
                <a href="%s" class="btn-sm" style="display: inline-block; padding: 8px 16px; font-size: 12px; font-weight: bold; color: #ffffff !important; background: #4f46e5; border-radius: 10px; text-decoration: none; transition: all 0.2s;" target="_blank">Corrigir no Painel &rarr;</a>
            </div>
        </div>
		`, item.HTTPMethod, item.JobName, item.URL, item.FailureCount, statusBadgeColor, statusStr, statusBadgeColor, statusBadgeText, item.LastResponseBody, jobJSON, btnLink)
	}

	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; background-color: #05070f; color: #f8fafc; margin: 0; padding: 40px 20px; text-align: center; }
        .card { max-width: 650px; margin: 0 auto; background: #0b0f19; border: 1px solid #1e293b; border-radius: 24px; padding: 36px; box-shadow: 0 20px 25px -5px rgba(0, 0, 0, 0.4); }
        .logo { font-size: 16px; font-weight: 800; color: #818cf8; text-transform: uppercase; letter-spacing: 0.15em; margin-bottom: 28px; }
        .alert-header { display: inline-flex; align-items: center; gap: 8px; padding: 6px 14px; background: rgba(245, 158, 11, 0.1); border: 1px solid rgba(245, 158, 11, 0.2); border-radius: 9999px; font-size: 11px; font-weight: 800; text-transform: uppercase; letter-spacing: 0.05em; color: #f59e0b; margin-bottom: 16px; }
        h2 { font-size: 22px; font-weight: 900; margin-top: 0; color: #f1f5f9; line-height: 1.3; text-align: left; }
        p { font-size: 14px; line-height: 1.6; color: #94a3b8; margin: 0 0 24px 0; text-align: left; }
        .footer { font-size: 11px; color: #475569; text-align: center; margin-top: 36px; border-top: 1px solid #1e293b; padding-top: 24px; line-height: 1.5; }
    </style>
</head>
<body>
    <div class="card">
        <div class="logo">CronFlow</div>
        <div class="alert-header">🚨 RESUMO DIÁRIO DE ERROS</div>
        <h2>Seu resumo de falhas das últimas 24 horas está pronto</h2>
        <p>Olá,</p>
        <p>Identificamos erros de execução em algumas de suas tarefas programadas hoje. Abaixo estão listados os jobs que falharam, juntamente com o número de falhas acumuladas nas últimas 24 horas, o último status de erro e a definição de cada tarefa:</p>
        
        %s

        <p class="footer">Este é um e-mail de resumo diário automático enviado pelo CronFlow Alertas.<br>Como usuário do plano gratuito, você recebe este consolidado diário no horário configurado no seu perfil.<br>Para receber alertas imediatos no exato instante da falha de um job, faça o upgrade para o plano <strong>PRO / Pago</strong>.<br>&copy; 2026 CronFlow. Todos os direitos reservados.</p>
    </div>
</body>
</html>`, cardsHTML)

	if s.resendKey != "" {
		err := s.sendViaResend(to, subject, body)
		if err != nil {
			return fmt.Errorf("MailService.SendDailyDigest (Resend): %w", err)
		}
		log.Printf("MailService: E-mail de resumo diário enviado com sucesso via Resend para %s", to)
		return nil
	}

	if s.host == "" || s.user == "" {
		log.Printf("\n========================================================================\n" +
			"[MOCK EMAIL] Envio de Resumo Diário (Sem SMTP configurado)\n" +
			"Para: %s\n" +
			"Assunto: %s\n" +
			"Total de Jobs Falhados: %d\n" +
			"========================================================================\n", to, subject, len(items))
		return nil
	}

	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	auth := smtp.PlainAuth("", s.user, s.pass, s.host)
	msg := []byte(fmt.Sprintf("To: %s\r\n"+
		"Subject: %s\r\n"+
		"MIME-version: 1.0;\r\n"+
		"Content-Type: text/html; charset=\"UTF-8\";\r\n\r\n"+
		"%s\r\n", to, subject, body))

	err := smtp.SendMail(addr, auth, s.from, []string{to}, msg)
	if err != nil {
		return fmt.Errorf("MailService.SendDailyDigest: %w", err)
	}

	log.Printf("MailService: E-mail de resumo diário enviado com sucesso para %s", to)
	return nil
}

// SendDowngradeWarningEmail envia um e-mail de aviso 3 dias antes do encerramento da assinatura PRO.
func (s *MailService) SendDowngradeWarningEmail(to, frontendURL string, daysRemaining, activeJobsCount, maxFreeLimit int) error {
	subject := fmt.Sprintf("⚠️ Aviso CronFlow: Sua assinatura PRO expira em %d dias", daysRemaining)

	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background-color: #04060f; color: #e2e8f0; margin: 0; padding: 40px 20px; }
        .container { max-width: 600px; margin: 0 auto; background: #0b0f19; border: 1px solid #1e1b4b; border-radius: 20px; padding: 32px; shadow: 0 25px 50px -12px rgba(0,0,0,0.5); }
        .header { text-align: center; border-b: 1px solid #1e1b4b; padding-bottom: 24px; margin-bottom: 24px; }
        .logo { font-size: 24px; font-weight: 800; color: #f59e0b; letter-spacing: -0.5px; }
        .badge { display: inline-block; padding: 6px 14px; background: rgba(245, 158, 11, 0.15); border: 1px solid rgba(245, 158, 11, 0.3); border-radius: 9999px; color: #fbbf24; font-size: 12px; font-weight: 700; text-transform: uppercase; margin-top: 12px; }
        .alert-box { background: rgba(245, 158, 11, 0.08); border: 1px solid rgba(245, 158, 11, 0.2); border-radius: 12px; padding: 16px; margin: 20px 0; font-size: 14px; line-height: 1.6; color: #fef08a; }
        .footer { text-align: center; font-size: 12px; color: #64748b; margin-top: 32px; border-top: 1px solid #1e1b4b; padding-top: 24px; }
        .btn { display: inline-block; padding: 12px 28px; background: #6366f1; color: #ffffff !important; text-decoration: none; font-weight: 700; border-radius: 12px; font-size: 14px; margin-top: 20px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <div class="logo">⚡ CronFlow</div>
            <div class="badge">Aviso de Renovação / Downgrade</div>
        </div>
        <h2 style="color: #f8fafc; font-size: 18px; margin-top: 0;">Sua assinatura PRO encerra em %d dias</h2>
        <p style="color: #94a3b8; font-size: 14px; line-height: 1.6;">
            Identificamos que sua assinatura do plano PRO está agendada para encerrar em <strong>%d dias</strong>.
        </p>
        <div class="alert-box">
            📌 <strong>O que acontecerá com seus agendamentos?</strong><br>
            Você possui atualmente <strong>%d tarefas ativas</strong>. Ao migrar para o plano Free (cota máxima de %d tarefas):
            <ul>
                <li>Seus <strong>%d agendamentos mais antigos</strong> permanecerão ativos.</li>
                <li>As <strong>%d tarefas excedentes</strong> serão automaticamente pausadas sem perda de dados.</li>
            </ul>
        </div>
        <p style="color: #94a3b8; font-size: 14px; line-height: 1.6;">
            Para manter todos os seus %d agendamentos ativos sem interrupções, reative sua assinatura do plano PRO.
        </p>
        <div style="text-align: center;">
            <a href="%s" class="btn">Renovar Plano PRO ✨</a>
        </div>
        <div class="footer">
            CronFlow Engine — Sistema Inteligente de Automação e Monitoramento SaaS
        </div>
    </div>
</body>
</html>`, daysRemaining, daysRemaining, activeJobsCount, maxFreeLimit, maxFreeLimit, activeJobsCount-maxFreeLimit, activeJobsCount, frontendURL)

	if s.resendKey != "" {
		return s.sendViaResend(to, subject, body)
	}

	if s.host == "" {
		log.Printf("MailService Mock (Downgrade Warning enviado para %s): %s", to, subject)
		return nil
	}

	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	auth := smtp.PlainAuth("", s.user, s.pass, s.host)
	msg := []byte(fmt.Sprintf("To: %s\r\n"+
		"Subject: %s\r\n"+
		"MIME-version: 1.0;\r\n"+
		"Content-Type: text/html; charset=\"UTF-8\";\r\n\r\n"+
		"%s\r\n", to, subject, body))

	return smtp.SendMail(addr, auth, s.from, []string{to}, msg)
}

