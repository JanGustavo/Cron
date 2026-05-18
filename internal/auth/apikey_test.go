package auth_test

import (
	"strings"
	"testing"

	"github.com/JanGustavo/Cron/internal/auth"
)

func TestGenerate(t *testing.T) {
	key, err := auth.Generate()
	if err != nil {
		t.Fatalf("Generate() retornou erro: %v", err)
	}

	if !strings.HasPrefix(key, "cf_live_") {
		t.Errorf("esperado prefixo cf_live_, got: %s", key)
	}

	// cf_live_ (8) + 64 hex chars (32 bytes) = 72
	if len(key) != 72 {
		t.Errorf("esperado 72 caracteres, got %d", len(key))
	}

	// Duas keys geradas nunca podem ser iguais
	key2, _ := auth.Generate()
	if key == key2 {
		t.Error("duas keys geradas foram idênticas — problema no gerador aleatório")
	}
}

func TestHashAndVerify(t *testing.T) {
	key, _ := auth.Generate()
	hash := auth.Hash(key)

	t.Run("key correta verifica com sucesso", func(t *testing.T) {
		if !auth.Verify(key, hash) {
			t.Error("Verify retornou false para key e hash corretos")
		}
	})

	t.Run("key errada não verifica", func(t *testing.T) {
		if auth.Verify("cf_live_chave_errada", hash) {
			t.Error("Verify retornou true para key inválida")
		}
	})

	t.Run("hash é determinístico", func(t *testing.T) {
		if auth.Hash(key) != hash {
			t.Error("mesmo input gerou hashes diferentes")
		}
	})

	t.Run("hash não é a key em plain text", func(t *testing.T) {
		if hash == key {
			t.Error("hash não pode ser igual à key original")
		}
	})
}