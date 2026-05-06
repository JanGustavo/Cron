package handler

// HealthHandler responde ao GET /health.
// Verifica conectividade com Postgres e Redis.
// Retorna: { "status": "ok", "postgres": "up", "redis": "up" }
// Não requer autenticação — endpoint público para load balancers.
