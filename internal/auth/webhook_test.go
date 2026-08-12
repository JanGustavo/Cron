package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestComputeWebhookSecret(t *testing.T) {
	projectID := "project_abc_123"
	jwtSecret := "my_jwt_super_secret_key"

	secret1 := ComputeWebhookSecret(projectID, jwtSecret)
	secret2 := ComputeWebhookSecret(projectID, jwtSecret)

	if secret1 != secret2 {
		t.Errorf("ComputeWebhookSecret deve ser determinístico, gerou segredos diferentes: %s e %s", secret1, secret2)
	}

	if !strings.HasPrefix(secret1, "whsec_") {
		t.Errorf("ComputeWebhookSecret deve começar com prefixo 'whsec_', got: %s", secret1)
	}

	// Comprimento do prefixo (6) + parte hash (24) = 30 caracteres
	if len(secret1) != 30 {
		t.Errorf("ComputeWebhookSecret deve ter exatamente 30 caracteres de tamanho, got: %d", len(secret1))
	}
}

func TestSignWebhookPayload(t *testing.T) {
	payload := []byte(`{"event":"job.failed","job_id":"job_11"}`)
	timestamp := time.Now().Unix()
	secret := "whsec_customsecret1234567890"

	signatureHeader := SignWebhookPayload(payload, timestamp, secret)

	// O formato do cabeçalho de assinatura de webhook deve ser t=<timestamp>,v1=<signature>
	if !strings.HasPrefix(signatureHeader, fmt.Sprintf("t=%d,v1=", timestamp)) {
		t.Errorf("Assinatura de webhook formatada incorretamente, got: %s", signatureHeader)
	}

	// Extrai a assinatura para re-verificar manualmente o HMAC
	parts := strings.Split(signatureHeader, ",v1=")
	if len(parts) != 2 {
		t.Fatalf("Assinatura não contém as partes esperadas, got: %s", signatureHeader)
	}
	extractedSig := parts[1]

	// Calcula manualmente para verificar
	mac := hmac.New(sha256.New, []byte(secret))
	signingPayload := fmt.Sprintf("%d.%s", timestamp, payload)
	mac.Write([]byte(signingPayload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if extractedSig != expectedSig {
		t.Errorf("A assinatura calculada não confere. Extraída: %s, Esperada: %s", extractedSig, expectedSig)
	}
}
