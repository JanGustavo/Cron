package scheduler

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"

	"github.com/JanGustavo/Cron/internal/database"
	"github.com/JanGustavo/Cron/internal/queue"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/repository/redis"
)

func TestSchedulerTickWithLock(t *testing.T) {
	// Carrega as variáveis do arquivo .env do projeto para conectar no banco real local
	_ = godotenv.Load("../../.env")

	runIntegration := os.Getenv("RUN_INTEGRATION_TESTS") == "true"

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgresql://admin:secretpassword@localhost:5433/cronflow?sslmode=disable"
	}
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6379"
	}

	db, err := database.Connect(dbURL)
	if err != nil {
		if runIntegration {
			t.Fatalf("Erro crítico: Postgres indisponível no ambiente de testes de integração: %v", err)
		} else {
			t.Skipf("Pulando teste: Postgres indisponível no ambiente local (defina RUN_INTEGRATION_TESTS=true para forçar erro): %v", err)
			return
		}
	}
	defer db.Close()

	lockRepo := redis.NewLockRepository(redisURL)
	defer lockRepo.Close()

	jobRepo := postgres.NewJobRepository(db)
	execRepo := postgres.NewExecutionRepository(db)
	enqueuer := queue.NewEnqueuer(redisURL)
	defer enqueuer.Close()

	// Inicializa o scheduler com intervalo dinâmico de 5 segundos
	interval := 5 * time.Second
	sched := New(jobRepo, execRepo, nil, enqueuer, lockRepo, interval)

	// Injeta uma hora fixa mockada para evitar flakiness próximo da troca de janelas
	mockTime := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	sched.SetNowFunc(func() time.Time { return mockTime })

	ctx := context.Background()

	// Calcula a chave de lock esperada para a época da hora mockada
	epochWindow := mockTime.Unix() / int64(interval.Seconds())
	lockKey := fmt.Sprintf("cronflow:scheduler:lock:%d", epochWindow)

	// Limpa qualquer lock antigo da época atual
	_ = lockRepo.Release(ctx, lockKey)

	// 1. Executa o tick
	sched.tick(ctx)

	// 2. Tenta obter o mesmo lock concorrentemente. Deve falhar porque a época atual já foi travada pelo scheduler.tick
	acquired, err := lockRepo.Acquire(ctx, lockKey, 5*time.Second)
	if err != nil {
		t.Fatalf("erro ao verificar aquisição de lock residual: %v", err)
	}
	if acquired {
		t.Errorf("erro: o lock da época %d deveria estar ocupado pelo scheduler, mas foi liberado/adquirido", epochWindow)
	}

	// 3. Libera o lock após o teste
	_ = lockRepo.Release(ctx, lockKey)
}
