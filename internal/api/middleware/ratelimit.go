package middleware

import (
	"net/http"
	"sync"
	"time"
)

// bucket controla tokens disponíveis por API Key.
type bucket struct {
	tokens    int
	lastReset time.Time
	mu        sync.Mutex
}

var (
	buckets   = make(map[string]*bucket)
	bucketsMu sync.RWMutex
)

// RateLimit limita a 60 requests por minuto por API Key.
// Usa token bucket in-memory — suficiente para o MVP com uma instância.
// Em múltiplas instâncias: migrar para Redis com INCR + EXPIRE.
func RateLimit(requestsPerMinute int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Authorization")
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			bucketsMu.RLock()
			b, exists := buckets[key]
			bucketsMu.RUnlock()

			if !exists {
				b = &bucket{tokens: requestsPerMinute, lastReset: time.Now()}
				bucketsMu.Lock()
				buckets[key] = b
				bucketsMu.Unlock()
			}

			b.mu.Lock()
			defer b.mu.Unlock()

			// Reseta tokens a cada minuto
			if time.Since(b.lastReset) >= time.Minute {
				b.tokens = requestsPerMinute
				b.lastReset = time.Now()
			}

			if b.tokens <= 0 {
				w.Header().Set("Retry-After", "60")
				writeUnauthorized(w, "rate limit excedido — 60 req/min por API Key")
				return
			}

			b.tokens--
			next.ServeHTTP(w, r)
		})
	}
}
