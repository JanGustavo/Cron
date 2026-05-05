package middleware

// LoggerMiddleware registra cada request HTTP em JSON estruturado.
// Captura: método, path, status code, duração e request_id.
// Usa slog (stdlib Go 1.21+) para output em stdout.

