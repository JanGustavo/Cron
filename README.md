# CronFlow — Plataforma de Agendamento e Automação de Tarefas

> **Stack:** Go 1.22 · PostgreSQL 16 · Redis 7 · Asynq · chi Router
> **Arquitetura:** Multi-binary · Stateless Workers · Distributed Scheduler
> **Princípio:** SRP (Single Responsibility Principle) aplicado em todos os níveis

---

## Sumário

1. [Visão Geral da Arquitetura](#visão-geral-da-arquitetura)
2. [Como Rodar Localmente](#como-rodar-localmente)
3. [Estrutura de Diretórios — Mapa Completo](#estrutura-de-diretórios--mapa-completo)
4. [cmd/ — Entrypoints (Binários)](#cmd--entrypoints-binários)
5. [internal/ — Código Privado da Aplicação](#internal--código-privado-da-aplicação)
6. [pkg/ — Pacotes Utilitários Reutilizáveis](#pkg--pacotes-utilitários-reutilizáveis)
7. [migrations/ — Schema e Queries SQL](#migrations--schema-e-queries-sql)
8. [deploy/ — Dockerfiles por Processo](#deploy--dockerfiles-por-processo)
9. [Decisões de Arquitetura](#decisões-de-arquitetura)
10. [Variáveis de Ambiente](#variáveis-de-ambiente)
11. [Limites do MVP](#limites-do-mvp)

---

## Visão Geral da Arquitetura

O CronFlow é composto por **3 processos independentes** que se comunicam via PostgreSQL e Redis:

```
┌─────────────────────────────────────────────────────────────┐
│                      CLIENTE (HTTPS)                        │
└──────────────────────────┬──────────────────────────────────┘
                           │ REST API
┌──────────────────────────▼──────────────────────────────────┐
│            PROCESSO 1: API (cmd/api)                        │
│         chi Router · Handlers · Services                    │
│         Autenticação via SHA-256(API Key)                   │
└────────────┬──────────────────────────┬────────────────────┘
             │ Leitura/Escrita          │ Fonte de Verdade
┌────────────▼──────────────┐  ┌───────▼────────────────────┐
│   PROCESSO 2: SCHEDULER   │  │     PostgreSQL 16           │
│   (cmd/scheduler)         │  │  T_USERS · T_PROJECTS      │
│   Loop 30s · Lock Redis   │  │  T_JOBS  · T_EXECUTIONS    │
│   Enfileira → Redis       │  └────────────────────────────┘
└────────────┬──────────────┘
             │ Enqueue
┌────────────▼──────────────────────────────────────────────┐
│              Redis 7 · Asynq Job Queue                     │
│        Filas: critical (paid) · default (free)             │
└────────────┬──────────────────────────────────────────────┘
             │ Consume (até 50 goroutines simultâneas)
┌────────────▼──────────────────────────────────────────────┐
│          PROCESSO 3: WORKER (cmd/worker)                   │
│    Executa HTTP Request → Salva Execution → Retry (3x)    │
│    Backoff Exponencial: 1min → 5min → 15min                │
│    3 falhas → DLQ → AlertService → Webhook do Usuário      │
└───────────────────────────────────────────────────────────┘
```

**Por que 3 processos separados?**
Porque eles têm requisitos operacionais diferentes:
- A **API** precisa de baixa latência e pode ter múltiplas réplicas
- O **Scheduler** precisa de exatamente 1 instância ativa (distributed lock)
- O **Worker** é stateless e escala horizontalmente sem limites

---

## Como Rodar Localmente

```bash
# 1. Clone e configure o ambiente
cp .env.example .env

# 2. Suba PostgreSQL e Redis
docker compose up -d postgres redis

# 3. Execute as migrations
make migrate/up

# 4. Em terminais separados, rode cada processo:
make dev/api        # Terminal 1 — API na porta 8080
make dev/scheduler  # Terminal 2 — Scheduler (loop 30s)
make dev/worker     # Terminal 3 — Worker Pool (50 goroutines)

# 5. Teste a API
curl http://localhost:8080/health
```

---

## Estrutura de Diretórios — Mapa Completo

```
cronflow/
│
├── cmd/                        ← ENTRYPOINTS — um por processo/binário
│   ├── api/
│   │   └── main.go             ← Inicia o servidor HTTP da API REST
│   ├── worker/
│   │   └── main.go             ← Inicia o pool de Workers Asynq
│   └── scheduler/
│       └── main.go             ← Inicia o loop do Scheduler
│
├── internal/                   ← Código PRIVADO — não importável por projetos externos
│   ├── config/
│   │   └── config.go           ← Lê variáveis de ambiente e expõe struct tipada
│   │
│   ├── domain/                 ← Entidades de domínio — ZERO dependências externas
│   │   ├── job/
│   │   │   └── job.go          ← Entidade Job + constantes de status + regras puras
│   │   ├── execution/
│   │   │   └── execution.go    ← Entidade Execution — resultado de cada disparo HTTP
│   │   ├── project/
│   │   │   └── project.go      ← Entidade Project (workspace multi-tenant)
│   │   └── user/
│   │       └── user.go         ← Entidade User + enum de planos (free/paid)
│   │
│   ├── repository/             ← Acesso ao banco — SQL isolado aqui
│   │   ├── postgres/
│   │   │   ├── job_repository.go        ← CRUD + FindEligibleToRun (query do Scheduler)
│   │   │   ├── execution_repository.go  ← Salvar resultado + histórico + retenção
│   │   │   └── user_repository.go       ← Auth: buscar project por hash da API Key
│   │   └── redis/
│   │       └── lock_repository.go       ← Distributed lock para o Scheduler
│   │
│   ├── api/                    ← Camada HTTP — roteamento, middlewares e handlers
│   │   ├── router/
│   │   │   └── router.go       ← Monta todas as rotas + aplica middlewares globais
│   │   ├── middleware/
│   │   │   ├── auth.go         ← Valida API Key → injeta Project no context
│   │   │   └── logger.go       ← Log estruturado JSON de cada request HTTP
│   │   └── handler/
│   │       ├── job_handler.go       ← Handlers do recurso Job (CRUD via HTTP)
│   │       ├── execution_handler.go ← Histórico de execuções com paginação
│   │       └── health_handler.go    ← GET /health — verifica Postgres e Redis
│   │
│   ├── service/                ← Lógica de negócio — orquestra repo + domínio
│   │   ├── job_service.go      ← Regras: limite de jobs por plano, cálculo next_run, etc
│   │   └── alert_service.go    ← Dispara webhook de alerta após 3 falhas consecutivas
│   │
│   ├── scheduler/
│   │   └── scheduler.go        ← Loop 30s: lê DB → enfileira Redis → atualiza next_run
│   │
│   ├── worker/
│   │   └── worker.go           ← Executa HTTP request → salva Execution → retry logic
│   │
│   ├── queue/
│   │   └── enqueuer.go         ← Abstração Asynq: cria e envia tasks tipadas para Redis
│   │
│   └── auth/
│       └── apikey.go           ← Generate / Hash(SHA-256) / Verify (timing-safe)
│
├── pkg/                        ← Pacotes PÚBLICOS — reutilizáveis por qualquer projeto
│   ├── cronparser/
│   │   └── parser.go           ← Valida cron expr + calcula next run + parse "every:Nm"
│   ├── httputil/
│   │   └── client.go           ← HTTP client reutilizável com timeout + body truncado
│   ├── logger/
│   │   └── logger.go           ← Setup slog: JSON em prod, colorido em dev
│   └── validator/
│       └── validator.go        ← Validação de structs → erros RFC 7807
│
├── migrations/                 ← Schema SQL versionado + queries para sqlc
│   ├── 001_create_users.up.sql    ← Cria: users, projects, api_keys
│   ├── 001_create_users.down.sql  ← Rollback da migration 001
│   ├── 002_create_jobs.up.sql     ← Cria: jobs (+ índice parcial), executions
│   ├── 002_create_jobs.down.sql   ← Rollback da migration 002
│   └── queries/                   ← Queries SQL puras para geração de código com sqlc
│       ├── jobs.sql               ← FindEligibleJobs, UpdateNextRun, IncrementFailures...
│       └── executions.sql         ← CreateExecution, ListByJob, DeleteOldExecutions
│
├── deploy/                     ← Dockerfiles — um por processo (multi-stage build)
│   ├── Dockerfile.api          ← Build otimizado: binário estático via scratch
│   ├── Dockerfile.worker       ← Mesmo padrão — imagem final ~5MB
│   └── Dockerfile.scheduler    ← Mesmo padrão
│
├── scripts/
│   └── gen_apikey.sh           ← Gera API Key + hash SHA-256 para testes locais
│
├── .github/
│   └── workflows/
│       └── ci.yml              ← CI: go vet + go test -race com Postgres e Redis reais
│
├── go.mod                      ← Módulo Go e dependências
├── go.sum                      ← Lock de versões (nunca editar manualmente)
├── sqlc.yaml                   ← Config do sqlc: qual SQL gera qual pacote Go
├── Makefile                    ← Atalhos: dev/api, migrate/up, sqlc/gen, test, build
├── docker-compose.yml          ← PostgreSQL 16 + Redis 7 para desenvolvimento local
└── .env.example                ← Template de variáveis de ambiente (commitar este)
```

---

## `cmd/` — Entrypoints (Binários)

> **Regra:** Cada `main.go` em `cmd/` representa um binário independente que pode ser deployado e escalado separadamente. **Nenhum `main.go` contém lógica de negócio.**

### `cmd/api/main.go`
Ponto de entrada do servidor HTTP. Sequência de boot:
1. Carrega `config.Config` do ambiente
2. Abre conexão com PostgreSQL e Redis
3. Instancia repositories → services → handlers
4. Monta o router chi com todas as rotas
5. Inicia `http.ListenAndServe` na porta configurada

### `cmd/worker/main.go`
Ponto de entrada do pool de Workers. Sequência de boot:
1. Conecta ao Redis via Asynq
2. Registra o handler `TypeHTTPJob → worker.Worker.ProcessTask`
3. Configura `MaxConcurrency: 50` e filas com prioridade
4. Inicia `asynq.Server.Run` (bloqueante)

### `cmd/scheduler/main.go`
Ponto de entrada do Scheduler. Sequência de boot:
1. Conecta ao PostgreSQL e Redis
2. Instancia `scheduler.Scheduler`
3. Inicia `scheduler.Run` — loop bloqueante com tick de 30s

---

## `internal/` — Código Privado da Aplicação

> **Por que `internal/`?** Em Go, pacotes dentro de `internal/` só podem ser importados por código dentro do mesmo módulo. Isso é enforçado pelo compilador — nenhum pacote externo pode importar nossa lógica de negócio acidentalmente.

### `internal/domain/` — As Entidades

A camada de domínio não conhece banco de dados, HTTP ou Redis. Só structs, constantes e métodos puros. Se você precisar adicionar uma lógica que depende de `database/sql`, ela não pertence ao domínio.

| Entidade | Responsabilidade |
|----------|-----------------|
| `job.Job` | Estrutura central. Contém schedule, URL, status, consecutive_failures |
| `execution.Execution` | Resultado de um disparo HTTP. Imutável após criado. |
| `project.Project` | Workspace de isolamento multi-tenant. |
| `user.User` | Dono da conta. Sem senha — auth é por API Key. |

### `internal/repository/` — Acesso ao Banco

Toda query SQL do projeto mora aqui. **Nenhuma outra camada escreve SQL.**

**`postgres/job_repository.go`** — a query mais crítica:
```sql
-- Chamada a cada 30s pelo Scheduler. Usa o índice parcial.
SELECT * FROM jobs
WHERE status = 'active' AND next_run_at <= NOW()
ORDER BY next_run_at ASC
LIMIT 500;
```

**`redis/lock_repository.go`** — previne double-enqueue:
```
SET scheduler:lock <nodeID> NX EX 40
```
Se retornar `0`, outro processo já tem o lock → skip o ciclo.

### `internal/api/` — A Camada HTTP

```
Request HTTP
    → router.go (qual handler?)
    → middleware/auth.go (API Key válida? → injeta Project no context)
    → middleware/logger.go (loga request)
    → handler/*.go (deserializa → chama service → serializa resposta)
```

**Regra dos Handlers:** Handlers são "thin controllers". Eles não tomam decisões de negócio. Se um handler tem mais de 30 linhas de lógica, algo está no lugar errado.

### `internal/service/` — Lógica de Negócio

Aqui mora o que não é banco e não é HTTP. Exemplos de regras no `JobService`:

```go
// Verificar limite de plano antes de criar job
count := repo.CountJobsByProject(projectID)
if user.Plan == "free" && count >= 5 {
    return ErrFreePlanJobLimit
}

// Calcular next_run_at no momento da criação
nextRun := cronparser.NextRun(job.Schedule, job.Timezone)
```

### `internal/scheduler/scheduler.go`

O Scheduler **não executa** HTTP requests. Ele só move jobs do PostgreSQL para a fila Redis. Isso é a separação de responsabilidade mais importante do sistema:

```
Scheduler responsabilidade: "Qual job deve rodar agora?"
Worker responsabilidade:    "Execute esse job específico."
```

Se o Scheduler executasse jobs diretamente, um job lento (timeout de 30s) travaria o loop inteiro, causando atrasos em cascata para todos os outros usuários.

### `internal/worker/worker.go`

Workers são **stateless**: recebem um `job_id`, buscam os dados no Postgres, fazem o request, salvam o resultado e encerram. Eles não guardam estado entre execuções.

O Asynq gerencia o retry automaticamente. O worker só precisa retornar `error` para sinalizar falha:

```go
func (w *Worker) ProcessTask(ctx context.Context, t *asynq.Task) error {
    // Se retornar erro, Asynq agenda retry com backoff exponencial
    // Após MaxRetry tentativas, move para Dead Letter Queue
}
```

---

## `pkg/` — Pacotes Utilitários Reutilizáveis

> **Diferença de `internal/`:** pacotes em `pkg/` não têm dependências de negócio e **poderiam** ser extraídos para um módulo separado no futuro. São ferramentas, não regras de negócio.

| Pacote | O que faz | Usado por |
|--------|-----------|-----------|
| `cronparser` | Valida e interpreta cron expressions | Service, Scheduler |
| `httputil` | Executa HTTP requests com timeout | Worker |
| `logger` | Setup slog com output JSON/colorido | Todos os processos |
| `validator` | Valida input de API → erros RFC 7807 | Handlers |

---

## `migrations/` — Schema e Queries SQL

O projeto usa **golang-migrate** para versionamento e **sqlc** para geração de código.

**Fluxo de desenvolvimento:**
```bash
# 1. Escreva a migration SQL
vim migrations/003_add_webhook_url.up.sql

# 2. Aplique
make migrate/up

# 3. Escreva as queries em /migrations/queries/
vim migrations/queries/jobs.sql

# 4. Gere o código Go tipado
make sqlc/gen
# → Gera internal/repository/postgres/db/*.go automaticamente
```

**Por que sqlc e não GORM?**
- GORM usa reflection em runtime → mais lento, erros em runtime
- sqlc valida SQL em tempo de compilação → erros em build, não em produção
- Você escreve SQL real → sem surpresas de N+1 queries geradas pelo ORM

### Índices críticos

```sql
-- Partial index: indexa SOMENTE jobs ativos.
-- Com 100k jobs onde 40% estão pausados, o índice tem 60% do tamanho total.
-- A query do Scheduler (a mais frequente do sistema) usa APENAS esse índice.
CREATE INDEX idx_jobs_scheduler ON jobs (next_run_at ASC)
    WHERE status = 'active';
```

---

## `deploy/` — Dockerfiles por Processo

Cada processo tem seu próprio Dockerfile usando **multi-stage build**:

```dockerfile
# Stage 1: compilação (imagem grande, ~800MB)
FROM golang:1.22-alpine AS builder
RUN go build -o /bin/api ./cmd/api

# Stage 2: imagem final (FROM scratch = zero OS)
FROM scratch
COPY --from=builder /bin/api /api
# Resultado: imagem de ~5MB com apenas o binário
```

Por que `FROM scratch`? O binário Go compilado com `CGO_ENABLED=0` é completamente estático — não precisa de libc, bash, nem nada. A imagem final não tem shell, o que também é uma vantagem de segurança.

---

## Decisões de Arquitetura

### Por que Go?
- Goroutines: 50 workers consomem ~200KB de RAM (vs ~100MB com threads OS)
- Binário estático: deploy é copiar um arquivo
- `net/http` stdlib já é production-grade
- Compilação rápida + detecção de race conditions com `-race`

### Por que PostgreSQL como fonte de verdade (e não Redis)?
- Redis é **efêmero** por design. Se o Redis cair e você só tiver os dados lá, perdeu tudo.
- PostgreSQL com ACID garante que um job nunca seja "perdido" entre o scheduler enfileirar e o worker processar.
- Se o Redis cair e o Scheduler reiniciar, ele reconstrói a fila consultando o Postgres.

### Por que Redis + Asynq para a fila?
- Redis tem latência sub-milissegundo — ideal para enfileirar dezenas de jobs em rajada
- Asynq implementa dead letter queue, retry com backoff e dashboard (Asynqmon) gratuitamente
- Alternativa (PostgreSQL como fila com `SKIP LOCKED`) é válida mas tem latência maior

### Por que API Keys com SHA-256 em vez de JWT?
- JWTs são stateless mas exigem rotação de segredo e têm complexidade de expiração
- API Keys são mais simples de revogar: delete o hash do banco, acesso negado imediatamente
- SHA-256 é suficiente para hashing de tokens aleatórios (não é senha — não precisa de bcrypt/argon2)

---

## Variáveis de Ambiente

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `APP_ENV` | `development` | `development` ou `production` |
| `APP_PORT` | `8080` | Porta do servidor HTTP |
| `DATABASE_URL` | — | Connection string PostgreSQL |
| `REDIS_URL` | `localhost:6379` | Endereço do Redis |
| `SCHEDULER_INTERVAL` | `30s` | Frequência do loop do Scheduler |
| `SCHEDULER_LOCK_TTL` | `40s` | TTL do distributed lock (deve ser > INTERVAL) |
| `WORKER_CONCURRENCY` | `50` | Goroutines paralelas máximas |
| `WORKER_TIMEOUT_DEFAULT` | `30s` | Timeout máximo por job |
| `MAX_JOBS_FREE_PLAN` | `5` | Limite de jobs no plano free |
| `MAX_JOBS_PAID_PLAN` | `100` | Limite de jobs no plano pago |
| `LOG_RETENTION_FREE_DAYS` | `7` | Dias de retenção de logs (free) |
| `LOG_RETENTION_PAID_DAYS` | `90` | Dias de retenção de logs (pago) |

---

## Limites do MVP

| Parâmetro | Valor | Motivo |
|-----------|-------|--------|
| Frequência mínima de job | 1 vez/minuto | Abaixo disso é real-time — problema diferente |
| Timeout máximo por job | 30 segundos | Protege workers de jobs zumbis |
| Payload máximo (POST) | 64 KB | Suficiente para 99% dos casos |
| Tentativas de retry | 3 | Balanceia confiabilidade vs custo |
| Workers simultâneos | 50 | Limite de infra inicial (ajustável) |
| Jobs por projeto (free) | 5 | Gate de conversão para plano pago |
| Jobs por projeto (pago) | 100 | Limite de negócio, não técnico |
| Rate limit da API | 60 req/min por API Key | Protege contra abuso |
| Retenção de logs (free) | 7 dias | Controle de custo de storage |
| Retenção de logs (pago) | 90 dias | Feature de diferenciação |

---

*CronFlow — Documento gerado para fins de arquitetura e referência do MVP*
