package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// ComputeWebhookSecret calcula um segredo de webhook estável e exclusivo para o projeto a partir da JWTSecret.
func ComputeWebhookSecret(projectID, jwtSecret string) string {
	mac := hmac.New(sha256.New, []byte(jwtSecret))
	mac.Write([]byte(projectID))
	return "whsec_" + hex.EncodeToString(mac.Sum(nil))[:24]
}

// SignWebhookPayload assina um payload HTTP utilizando o timestamp e o segredo de webhook do projeto.
// Segue o padrão de mercado (Stripe/Svix): X-CronFlow-Signature: t=<timestamp>,v1=<signature>.
func SignWebhookPayload(payload []byte, timestamp int64, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	signingPayload := fmt.Sprintf("%d.%s", timestamp, payload)
	mac.Write([]byte(signingPayload))
	signature := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("t=%d,v1=%s", timestamp, signature)
}
