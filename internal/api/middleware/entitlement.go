package middleware

import (
	"encoding/json"
	"net/http"

	"github.com/JanGustavo/Cron/internal/service"
)

// RequireJobEntitlement checks if the user is allowed to create another job before proceeding.
func RequireJobEntitlement(engine *service.EntitlementEngine) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			proj := ProjectFromContext(r.Context())
			if proj == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{
					"error":  "unauthorized",
					"reason": "Autorização ausente ao validar direitos",
				})
				return
			}

			if err := engine.CheckJobCreation(r.Context(), proj.UserID); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusPaymentRequired)
				json.NewEncoder(w).Encode(map[string]string{
					"error": err.Error(),
					"code":  "LIMIT_EXCEEDED",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
