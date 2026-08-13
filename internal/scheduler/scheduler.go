package scheduler

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/JanGustavo/Cron/internal/queue"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/repository/redis"
	"github.com/JanGustavo/Cron/internal/service"
	"github.com/JanGustavo/Cron/pkg/cronparser"
)

// Scheduler — coração do sistema de agendamento.
// Loop a cada 30s:
//   1. SELECT jobs WHERE next_run_at <= NOW() AND status = 'active'
//   2. Para cada job: enfileirar no Redis via Asynq
//   3. UPDATE jobs SET next_run_at = <próximo horário>
//
// REGRA CRÍTICA: o Scheduler NUNCA executa HTTP requests.
// Apenas lê o banco e escreve na fila.

type Scheduler struct {
	jobRepo       *postgres.JobRepository
	executionRepo *postgres.ExecutionRepository
	alertService  *service.AlertService
	enqueuer      *queue.Enqueuer
	lockRepo      *redis.LockRepository
	interval      time.Duration
	cleanupTick   int
	nowFunc       func() time.Time
}

func New(
	jobRepo *postgres.JobRepository,
	executionRepo *postgres.ExecutionRepository,
	alertService *service.AlertService,
	enqueuer *queue.Enqueuer,
	lockRepo *redis.LockRepository,
	interval time.Duration,
) *Scheduler {
	return &Scheduler{
		jobRepo:       jobRepo,
		executionRepo: executionRepo,
		alertService:  alertService,
		enqueuer:      enqueuer,
		lockRepo:      lockRepo,
		interval:      interval,
		nowFunc:       func() time.Time { return time.Now().UTC() },
	}
}

// SetNowFunc allows injecting a mock clock for testing
func (s *Scheduler) SetNowFunc(nowFunc func() time.Time) {
	s.nowFunc = nowFunc
}

// Run inicia o loop principal do Scheduler — bloqueante, roda até o ctx ser cancelado.
// Em produção: ctx vem de signal.NotifyContext para graceful shutdown no Ctrl+C.
func (s *Scheduler) Run(ctx context.Context) {
	log.Printf("Scheduler iniciado — tick a cada %s", s.interval)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Executa imediatamente no boot, sem esperar o primeiro tick
	s.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Println("Scheduler encerrado")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick é a unidade de trabalho: busca jobs elegíveis, enfileira e atualiza next_run.
func (s *Scheduler) tick(ctx context.Context) {
	if s.alertService != nil {
		s.alertService.ProcessDailyDigests(ctx)
	}

	now := s.nowFunc()

	if s.lockRepo != nil {
		intervalSeconds := int64(s.interval.Seconds())
		if intervalSeconds <= 0 {
			intervalSeconds = 30
		}
		epochWindow := now.Unix() / intervalSeconds
		lockKey := fmt.Sprintf("cronflow:scheduler:lock:%d", epochWindow)

		lockTTL := s.interval + 10*time.Second
		if lockTTL < 15*time.Second {
			lockTTL = 15 * time.Second // garante um TTL mínimo razoável no Redis
		}

		acquired, err := s.lockRepo.Acquire(ctx, lockKey, lockTTL)
		if err != nil {
			log.Printf("Scheduler.tick: erro ao obter lock no Redis: %v", err)
		} else if !acquired {
			// Outra instância já executou ou está executando neste bloco de tempo
			return
		}
	}

	jobs, err := s.jobRepo.FindEligibleToRun(ctx, now)
	if err != nil {
		log.Printf("Scheduler.tick: erro ao buscar jobs: %v", err)
	} else if len(jobs) > 0 {
		log.Printf("Scheduler.tick: %d job(s) elegíveis", len(jobs))

		for _, j := range jobs {
			// Calcula próxima execução ANTES de enfileirar
			// Se o enqueue falhar, o next_run já está correto para o próximo ciclo
			nextRun, err := cronparser.NextRun(j.Schedule, j.Timezone, now)
			if err != nil {
				log.Printf("Scheduler.tick: erro ao calcular next_run do job %s: %v", j.ID, err)
				continue
			}

			// Enfileira no Redis via Asynq
			if err := s.enqueuer.Enqueue(ctx, j.ID); err != nil {
				log.Printf("Scheduler.tick: erro ao enfileirar job %s: %v", j.ID, err)
				continue
			}

			// Atualiza next_run_at no banco
			if err := s.jobRepo.UpdateNextRun(ctx, j.ID, nextRun); err != nil {
				log.Printf("Scheduler.tick: erro ao atualizar next_run do job %s: %v", j.ID, err)
				continue
			}

			log.Printf("Scheduler.tick: job %s enfileirado — próxima execução: %s", j.ID, nextRun.Format(time.RFC3339))
		}
	}

	// Limpeza a cada 2880 ciclos = ~24h (2880 × 30s)
	s.cleanupTick++
	if s.cleanupTick >= 2880 {
		s.cleanupTick = 0
		deleted, err := s.executionRepo.DeleteOlderThan(ctx, 7)
		if err != nil {
			log.Printf("Scheduler: erro na limpeza de logs: %v", err)
		} else if deleted > 0 {
			log.Printf("Scheduler: limpeza — %d execuções antigas removidas", deleted)
		}
	}
}
