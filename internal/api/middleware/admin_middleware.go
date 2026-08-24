package middleware

import (
	"context"
	"net/http"

	"github.com/JanGustavo/Cron/internal/domain/user"
)

type UserRepoForAdmin interface {
	FindUserByProjectID(ctx context.Context, projectID string) (*user.User, error)
}

func RequireAdmin(userRepo UserRepoForAdmin) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			proj := ProjectFromContext(r.Context())
			if proj == nil {
				http.Error(w, `{"error":"Não autorizado","code":"UNAUTHORIZED"}`, http.StatusUnauthorized)
				return
			}

			u, err := userRepo.FindUserByProjectID(r.Context(), proj.ID)
			if err != nil || u == nil {
				http.Error(w, `{"error":"Usuário não encontrado","code":"NOT_FOUND"}`, http.StatusNotFound)
				return
			}

			if u.Role != "admin" {
				http.Error(w, `{"error":"Acesso restrito a administradores do sistema","code":"FORBIDDEN"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
