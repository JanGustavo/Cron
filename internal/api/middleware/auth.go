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

// Auth retorna um middleware chi que valida a API Key ou Token JWT em toda request.
// Fluxo:
//  1. Lê o cabeçalho "Authorization: Bearer <token>"
//  2. Se começar com "cf_live_", trata como API Key tradicional (faz hash e consulta DB)
//  3. Senão, decodifica e valida criptograficamente como Token JWT (não encosta no banco, super rápido!)
//  4. Injeta o *project.Project no context para os handlers usarem
func Auth(userRepo *postgres.UserRepository, jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				writeUnauthorized(w, "Authorization header ausente ou inválido")
				return
			}

			var proj *project.Project
			var err error

			if strings.HasPrefix(token, "cf_live_") {
				// Autenticação tradicional via API Key (exclusivo para SDKs / Integrações cURL)
				keyHash := auth.Hash(token)
				proj, err = userRepo.FindProjectByKeyHash(r.Context(), keyHash)
				if err != nil {
					writeUnauthorized(w, "erro interno ao validar API Key")
					return
				}
				if proj == nil {
					writeUnauthorized(w, "API Key inválida ou revogada")
					return
				}
			} else {
				// Autenticação em tempo real via Token JWT (Painel do Desenvolvedor)
				claims, err := auth.ValidateToken(token, jwtSecret)
				if err != nil {
					writeUnauthorized(w, "Token JWT inválido ou expirado")
					return
				}
				proj = &project.Project{
					ID:     claims.ProjectID,
					UserID: claims.UserID,
					Name:   "Projeto Autenticado via JWT",
				}
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