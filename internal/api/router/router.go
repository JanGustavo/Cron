package router

// Router define e monta todas as rotas da API REST usando chi.
// Aplica middlewares globais: Logger, Recoverer, RealIP, AuthMiddleware.
//
// Rotas do MVP:
//   GET  /health
//   POST /v1/auth/keys
//   GET  /v1/jobs
//   POST /v1/jobs
//   GET  /v1/jobs/{id}
//   PATCH /v1/jobs/{id}
//   DELETE /v1/jobs/{id}
//   GET  /v1/jobs/{id}/executions
