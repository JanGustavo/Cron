package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// LockRepository implementa distributed locking via Redis SET NX EX.
// Garante que apenas UMA instância do Scheduler execute o loop
// em um dado momento (previne double-enqueue em deploys horizontais).
type LockRepository struct {
	client *redis.Client
}

func NewLockRepository(addr string) *LockRepository {
	opt, err := redis.ParseURL(addr)
	var client *redis.Client
	if err != nil {
		client = redis.NewClient(&redis.Options{
			Addr: addr,
		})
	} else {
		client = redis.NewClient(opt)
	}

	return &LockRepository{client: client}
}

// Acquire tenta adquirir um lock com um TTL específico.
// Retorna true se adquiriu o lock com sucesso (SET NX).
func (r *LockRepository) Acquire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return r.client.SetNX(ctx, key, "locked", ttl).Result()
}

// Release remove o lock.
func (r *LockRepository) Release(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

// Close fecha o client do Redis.
func (r *LockRepository) Close() error {
	return r.client.Close()
}
