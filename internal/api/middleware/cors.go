package middleware

import "net/http"

// CORS é um middleware que gerencia requisições preflight OPTIONS e cabeçalhos Access-Control.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Permite qualquer origem para o MVP local
		w.Header().Set("Access-Control-Allow-Origin", "*")
		
		// Métodos permitidos
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		
		// Cabeçalhos que o cliente pode enviar
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

		// Se for uma requisição preflight (OPTIONS), responde com 204 No Content imediatamente
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
