package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type rateBucket struct {
	tokens    int
	lastReset time.Time
	mu        sync.Mutex
}

type RateLimiter struct {
	buckets map[string]*rateBucket
	mu      sync.RWMutex
	rate    int
}

var (
	globalLimiter = &RateLimiter{
		buckets: make(map[string]*rateBucket),
		rate:    300,
	}
)

// RateLimit aplica controle de taxa tanto por Token de autenticação quanto por Real IP do cliente.
// Nunca permite bypass de requisições desautenticadas.
func RateLimit(requestsPerMinute int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Determina a chave do cliente: Token Authorization ou Real IP
			key := r.Header.Get("Authorization")
			if key == "" {
				// Fallback seguro: extrai IP do cliente (considerando cabeçalhos de proxy do Chi)
				ip, _, err := net.SplitHostPort(r.RemoteAddr)
				if err != nil {
					ip = r.RemoteAddr
				}
				if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
					ip = realIP
				} else if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
					ip = strings.TrimSpace(strings.Split(forwarded, ",")[0])
				}
				key = "ip:" + ip
			}

			globalLimiter.mu.RLock()
			b, exists := globalLimiter.buckets[key]
			globalLimiter.mu.RUnlock()

			if !exists {
				b = &rateBucket{tokens: requestsPerMinute, lastReset: time.Now()}
				globalLimiter.mu.Lock()
				globalLimiter.buckets[key] = b
				globalLimiter.mu.Unlock()
			}

			b.mu.Lock()
			// Reseta a janela a cada 1 minuto
			if time.Since(b.lastReset) >= time.Minute {
				b.tokens = requestsPerMinute
				b.lastReset = time.Now()
			}

			if b.tokens <= 0 {
				b.mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "too_many_requests",
					"message": "Limite de requisições excedido. Tente novamente em 1 minuto.",
				})
				return
			}

			b.tokens--
			b.mu.Unlock()

			next.ServeHTTP(w, r)
		})
	}
}
