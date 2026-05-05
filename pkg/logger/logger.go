package logger

// Logger — configura slog (stdlib Go 1.21+).
// Produção: output JSON para stdout (coletado pelo Railway/Fly.io).
// Desenvolvimento: output colorido/legível.
// Exporta helpers com campos contextuais: job_id, project_id, request_id.
// NUNCA usar fmt.Println em código de produção.

