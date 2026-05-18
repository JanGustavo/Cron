package auth

// APIKey — geração e verificação de API Keys.
//   - Generate() → "cf_live_" + 32 bytes hex aleatório
//   - Hash(key) → SHA-256(key) para armazenar no banco
//   - Verify(key, hash) → comparação timing-safe (hmac.Equal)
//
// REGRA DE OURO: plain text NUNCA é persistido.
// Banco armazena APENAS o hash.

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
)

const prefix = "cf_live_" // Prefixo para identificar chaves geradas por este sistema

// Generate cria uma nova API Key aleatória e criptograficamente segura.
// Formato: "cf_live_" + 32 bytes em hex = 72 caracteres no total.
// Retorna a key em plain text — é a ÚNICA vez que ela aparece assim.
// O chamador é responsável por mostrar pro usuário e nunca mais armazenar.
func Generate() (string, error){
	bytes := make([]byte, 32) // 32 bytes = 256 bits de entropia
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("falha ao gerar bytes aleatórios: %w", err)
	}

	return prefix + hex.EncodeToString(bytes), nil
}

// Hash calcula SHA-256 da key e retorna em hex.
// É o valor salvo no banco — nunca o plain text.
// SHA-256 é suficiente aqui porque a key já é aleatória com 256 bits de entropia.
// (diferente de senhas de usuário, que precisam de bcrypt/argon2)
func Hash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}	

// Verify compara a key recebida na request com o hash armazenado no banco.
// Usa subtle.ConstantTimeCompare para prevenir timing attacks:
// sem isso, um atacante poderia medir o tempo de resposta e deduzir caracteres da key.
func Verify(key, storedHash string) bool {
	incoming := Hash(key)
	return subtle.ConstantTimeCompare([]byte(incoming), []byte(storedHash)) == 1
}