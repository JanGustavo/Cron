package main

// Entrypoint do Scheduler (instância única global).
// Responsabilidade: adquirir distributed lock no Redis,
// executar o loop de 30s consultando jobs com next_run_at <= NOW()
// e enfileirá-los no Redis via Asynq.

func main() {}
