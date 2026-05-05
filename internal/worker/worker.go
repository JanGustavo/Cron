package worker

// Worker — executor HTTP das tasks enfileiradas.
// Fluxo por task:
//   1. Ler task da fila Redis (Asynq)
//   2. Buscar detalhes do job no Postgres
//   3. Executar HTTP request com timeout (max 30s)
//   4. Registrar Execution no Postgres
//   5. Falha → Asynq faz retry (backoff: 1min → 5min → 15min)
//   6. 3 falhas → Dead Letter Queue → AlertService
//
// Workers são completamente STATELESS.
// Escala horizontal: adicionar réplicas sem coordenação.
// Concorrência máxima MVP: 50 goroutines.

type Worker struct{}
