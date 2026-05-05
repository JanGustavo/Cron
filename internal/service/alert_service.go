package service

// AlertService — disparo de alertas de falha.
// Após consecutive_failures == 3: dispara HTTP POST para a
// webhook_url do usuário com payload de diagnóstico.
// Roda de forma assíncrona (goroutine) para não bloquear o Worker.

type AlertService struct{}
