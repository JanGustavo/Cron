package handler

// ExecutionHandler — handlers HTTP para histórico de execuções.
// GET /v1/jobs/{id}/executions?limit=50&cursor=<timestamp>
// Retorna JSON com array de execuções e next_cursor para paginação.

type ExecutionHandler struct{}
