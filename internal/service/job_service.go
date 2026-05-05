package service

// JobService — lógica de negócio para Jobs.
// Regras aplicadas:
//   - Validar cron expression antes de salvar
//   - Calcular first next_run_at ao criar job
//   - Verificar limite de jobs por projeto (5 free / 100 paid)
//   - Ao pausar: atualizar status sem deletar next_run_at
//   - Ao reativar: recalcular next_run_at a partir de agora
// É a única camada que os Handlers conhecem.

type JobService struct{}
