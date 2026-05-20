package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/JanGustavo/Cron/internal/auth"
	"github.com/JanGustavo/Cron/internal/domain/project"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
)

// contextKey é um tipo privado para chaves de context.
// Usar string pura como chave de context é erro comum em Go —
// qualquer pacote poderia sobrescrever acidentalmente com a mesma string.
type contextKey string

const projectContextKey contextKey = "project"

// Auth retorna um middleware chi que valida a API Key em toda request.
// Fluxo:
//  1. Lê "Authorization: Bearer cf_live_..."
//  2. Calcula SHA-256 da key
//  3. Consulta o banco — se não achar, 401
//  4. Injeta o *project.Project no context para os handlers usarem
func Auth(userRepo *postgres.UserRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extrai a key do header
			apiKey := extractBearerToken(r)
			if apiKey == "" {
				writeUnauthorized(w, "Authorization header ausente ou inválido")
				return
			}

			// Calcula hash e consulta o banco
			keyHash := auth.Hash(apiKey)
			proj, err := userRepo.FindProjectByKeyHash(r.Context(), keyHash)
			if err != nil {
				writeUnauthorized(w, "erro interno ao validar credenciais")
				return
			}
			if proj == nil {
				writeUnauthorized(w, "API Key inválida")
				return
			}

			// Injeta o projeto no context — handlers leem com ProjectFromContext()
			ctx := context.WithValue(r.Context(), projectContextKey, proj)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ProjectFromContext extrai o projeto autenticado do context.
// Retorna nil se chamado fora de uma rota protegida — nunca deve acontecer
// se o middleware estiver aplicado corretamente no router.
func ProjectFromContext(ctx context.Context) *project.Project {
	proj, _ := ctx.Value(projectContextKey).(*project.Project)
	return proj
}

// extractBearerToken lê o header Authorization e retorna só a key.
// "Bearer cf_live_abc123" → "cf_live_abc123"
func extractBearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// writeUnauthorized escreve uma resposta 401 padronizada.
func writeUnauthorized(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{
		"error":  "unauthorized",
		"reason": reason,
	})
}