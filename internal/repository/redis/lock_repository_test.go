package redis

import (
	"context"
	"testing"
	"time"
)

func TestLockRepository(t *testing.T) {
	ctx := context.Background()
	addr := "localhost:6379"

	repo := NewLockRepository(addr)
	defer repo.Close()

	key := "cronflow:test:lock:key"

	// Garante que o lock esteja limpo no início do teste
	_ = repo.Release(ctx, key)

	// 1. Primeira aquisição deve ter sucesso
	acquired, err := repo.Acquire(ctx, key, 2*time.Second)
	if err != nil {
		t.Fatalf("erro ao adquirir primeiro lock: %v", err)
	}
	if !acquired {
		t.Errorf("esperava adquirir o lock, mas retornou falso")
	}

	// 2. Segunda tentativa de aquisição concorrente deve falhar (lock ocupado)
	acquired2, err := repo.Acquire(ctx, key, 2*time.Second)
	if err != nil {
		t.Fatalf("erro na segunda tentativa de lock: %v", err)
	}
	if acquired2 {
		t.Errorf("esperava falhar ao adquirir lock que já está ocupado")
	}

	// 3. Liberar o lock
	err = repo.Release(ctx, key)
	if err != nil {
		t.Fatalf("erro ao liberar lock: %v", err)
	}

	// 4. Nova tentativa após liberação deve ter sucesso
	acquired3, err := repo.Acquire(ctx, key, 2*time.Second)
	if err != nil {
		t.Fatalf("erro ao re-adquirir lock liberado: %v", err)
	}
	if !acquired3 {
		t.Errorf("esperava conseguir adquirir o lock liberado")
	}

	// Limpa o lock após o teste terminar
	_ = repo.Release(ctx, key)
}
