package scheduler

// Scheduler — coração do sistema de agendamento.
// Loop a cada 30s:
//   1. Adquirir distributed lock no Redis
//   2. SELECT jobs WHERE next_run_at <= NOW() AND status = 'active'
//   3. Para cada job: enfileirar no Redis via Asynq
//   4. UPDATE jobs SET next_run_at = <próximo horário>
//   5. Renovar o lock antes de expirar
//
// REGRA CRÍTICA: o Scheduler NUNCA executa HTTP requests.
// Apenas lê o banco e escreve na fila.

type Scheduler struct{}
