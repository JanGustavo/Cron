package postgres

// UserRepository — acesso ao banco para User e APIKey.
// NUNCA armazena ou retorna a API Key em plain text.
// Métodos:
//   - Create(ctx, user)
//   - FindByEmail(ctx, email)
//   - FindProjectByKeyHash(ctx, hash)  ← autenticação

type UserRepository struct{}
