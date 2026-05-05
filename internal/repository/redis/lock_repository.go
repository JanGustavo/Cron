package redis

// LockRepository implementa distributed locking via Redis SET NX EX.
// Garante que apenas UMA instância do Scheduler execute o loop
// em um dado momento (previne double-enqueue em deploys horizontais).
// TTL de 40s > intervalo de 30s do loop = renovação antes de expirar.

type LockRepository struct{}
