package auth_test

import (
	"testing"
	"time"

	"github.com/JanGustavo/Cron/internal/auth"
)

func TestJWTTokenGegerationAndValidation(t *testing.T) {
	secret := "test_secret_for_cronflow_jwt_unit_tests"
	userID := "user-123"
	email := "test@cronflow.com"
	projectID := "project-456"
	plan := "free"

	t.Run("gera e valida token com sucesso", func(t *testing.T) {
		token, err := auth.GenerateToken(userID, email, projectID, plan, secret, 5*time.Second)
		if err != nil {
			t.Fatalf("GenerateToken retornou erro: %v", err)
		}

		claims, err := auth.ValidateToken(token, secret)
		if err != nil {
			t.Fatalf("ValidateToken falhou com token válido: %v", err)
		}

		if claims.UserID != userID {
			t.Errorf("esperado UserID %s, got: %s", userID, claims.UserID)
		}
		if claims.Email != email {
			t.Errorf("esperado Email %s, got: %s", email, claims.Email)
		}
		if claims.ProjectID != projectID {
			t.Errorf("esperado ProjectID %s, got: %s", projectID, claims.ProjectID)
		}
		if claims.Plan != plan {
			t.Errorf("esperado Plan %s, got: %s", plan, claims.Plan)
		}
	})

	t.Run("token com secret errada deve falhar na validacao", func(t *testing.T) {
		token, _ := auth.GenerateToken(userID, email, projectID, plan, secret, 5*time.Second)
		_, err := auth.ValidateToken(token, "wrong_secret_123")
		if err == nil {
			t.Error("esperava-se que a validação falhasse com secret incorreta, mas passou")
		}
	})

	t.Run("token expirado deve falhar na validacao", func(t *testing.T) {
		token, _ := auth.GenerateToken(userID, email, projectID, plan, secret, -1*time.Second)
		_, err := auth.ValidateToken(token, secret)
		if err == nil {
			t.Error("esperava-se que o token expirado falhasse na validação, mas passou")
		}
	})
}

func TestPasswordHashing(t *testing.T) {
	password := "CronFlowSecDev2026!"

	t.Run("hasheia e verifica senha com sucesso", func(t *testing.T) {
		hash, err := auth.HashPassword(password)
		if err != nil {
			t.Fatalf("HashPassword falhou: %v", err)
		}

		if hash == password {
			t.Error("o hash resultante não deve ser igual à senha plain text")
		}

		if !auth.CheckPasswordHash(password, hash) {
			t.Error("CheckPasswordHash falhou com a senha correta")
		}
	})

	t.Run("senha errada deve falhar na verificacao", func(t *testing.T) {
		hash, _ := auth.HashPassword(password)
		if auth.CheckPasswordHash("senha_errada_total", hash) {
			t.Error("CheckPasswordHash passou para uma senha inválida")
		}
	})
}
