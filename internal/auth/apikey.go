package auth

// APIKey — geração e verificação de API Keys.
//   - Generate() → "cf_live_" + 32 bytes hex aleatório
//   - Hash(key) → SHA-256(key) para armazenar no banco
//   - Verify(key, hash) → comparação timing-safe (hmac.Equal)
//
// REGRA DE OURO: plain text NUNCA é persistido.
// Banco armazena APENAS o hash.
