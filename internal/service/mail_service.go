package service

import (
	"fmt"
	"log"
	"net/smtp"
)

type MailService struct {
	host string
	port int
	user string
	pass string
	from string
}

func NewMailService(host string, port int, user, pass, from string) *MailService {
	return &MailService{
		host: host,
		port: port,
		user: user,
		pass: pass,
		from: from,
	}
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
