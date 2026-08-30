package middleware

import (
	"net/http"
	"os"
	"strings"
)

// CORS aplica validação de origens permitidas baseando-se no ambiente e variáveis de configuração.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		appEnv := os.Getenv("APP_ENV")
		frontendURL := os.Getenv("FRONTEND_URL")

		// Em desenvolvimento local, permite localhost/127.0.0.1
		if appEnv != "production" {
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			} else {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}
		} else {
			// Em produção, restringe estritamente ao domínio oficial e subdomínios permitidos
			allowedOrigins := []string{
				"https://cronflow.jangustavo.me",
				"https://api.cronflow.jangustavo.me",
			}
			if frontendURL != "" {
				allowedOrigins = append(allowedOrigins, frontendURL)
			}

			isAllowed := false
			for _, allowed := range allowedOrigins {
				if strings.EqualFold(origin, allowed) {
					isAllowed = true
					break
				}
			}

			if isAllowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			} else if origin == "" {
				// Requisições diretas de cURL/SDK sem header Origin
				w.Header().Set("Access-Control-Allow-Origin", "https://cronflow.jangustavo.me")
			}
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
