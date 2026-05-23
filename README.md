# 🚀 CronFlow — Plataforma de Agendamento e Automação de Tarefas
## Manual de Desenvolvimento e Arquitetura do MVP (Para Desenvolvedores Juniores e Estagiários)

Bem-vindo ao time de engenharia do **CronFlow**! Este guia foi feito sob medida para ajudar você a entender o funcionamento interno de nosso sistema, desde a arquitetura de múltiplos binários até os fluxos de dados mais complexos.

O CronFlow é uma plataforma SaaS de agendamento de tarefas e disparo de webhooks (um "CronTab em escala como serviço"). O sistema permite que nossos usuários cadastrem requisições HTTP agendadas (via expressões cron) que devem ser executadas com alta precisão, tolerância a falhas e total rastreabilidade.

---

## 🗺️ Visão Geral da Arquitetura (Múltiplos Binários)

Em vez de construirmos um monólito gigante e pesado, o CronFlow foi projetado seguindo o **Princípio da Responsabilidade Única (SRP)** no nível de infraestrutura. O sistema é dividido em **3 processos totalmente independentes** (binários distintos) que rodam lado a lado, comunicando-se através de duas fontes: o banco de dados **PostgreSQL** e o broker de mensagens **Redis**.

```
┌─────────────────────────────────────────────────────────────┐
│                      CLIENTE (HTTPS)                        │
└──────────────────────────┬──────────────────────────────────┘
                           │ REST API
┌──────────────────────────▼──────────────────────────────────┐
│            PROCESSO 1: API REST (cmd/api)                   │
│         chi Router · Handlers · Services                    │
│         Autenticação via SHA-256 (API Keys)                 │
└────────────┬──────────────────────────┬────────────────────┘
             │ Leitura/Escrita          │ Fonte da Verdade
┌────────────▼──────────────┐  ┌───────▼────────────────────┐
│   PROCESSO 2: SCHEDULER   │  │     PostgreSQL 16          │
│   (cmd/scheduler)         │  │   Tabelas de Usuários,     │
│   Loop 30s · Lock Redis   │  │   Projetos, Jobs e Logs    │
│   Enfileira → Redis       │  └────────────────────────────┘
└────────────┬──────────────┘
             │ Enqueue
┌────────────▼──────────────────────────────────────────────┐
│              Redis 7 · Fila de Tarefas (Asynq)             │
│        Filas prioritárias e agendamentos temporários       │
└────────────┬──────────────────────────────────────────────┘
             │ Consume (até 50 goroutines simultâneas)
┌────────────▼──────────────────────────────────────────────┐
│            PROCESSO 3: WORKER (cmd/worker)                  │
│    Executa Requisição HTTP → Salva Log → Retry (3x)        │
│    Backoff Exponencial: 1min → 5min → 15min                │
│    Se falhar 3x → DLQ → Dispara Alerta via Webhook         │
└────────────────────────────────────────────────────────────┘
```

### Por que separamos em 3 processos?
1. **API (`cmd/api`)**: Precisa de alta escalabilidade e baixa latência. Pode ser multiplicada em várias réplicas conforme a quantidade de requisições de clientes crescer.
2. **Scheduler (`cmd/scheduler`)**: É o coração do agendamento. Para evitar execuções duplicadas, **só pode haver exatamente 1 instância ativa** rodando (usamos locks distribuídos no Redis para garantir isso).
3. **Worker (`cmd/worker`)**: É totalmente *stateless* (não guarda estado). Ele pode ser escalado horizontalmente de forma agressiva para aguentar rajadas massivas de execuções HTTP sem afetar a performance da API.

---

## 🗂️ Glossário de Domínio (As Entidades)

Antes de abrir o código, você precisa entender os termos do nosso domínio de negócios. Eles estão mapeados em `internal/domain/`:

*   **User (Usuário)**: A conta principal do cliente cadastrado. Controla qual **plano** de assinatura (ex: `free` ou `paid`) está ativo. Não usamos senhas; a autenticação no sistema é feita exclusivamente via **API Key**.
*   **Project (Projeto/Workspace)**: O ambiente (tenant) isolado de trabalho de um usuário. Um usuário pode criar múltiplos projetos para separar seus ambientes (ex: `produção`, `homologação`).
*   **Job (Tarefa Agendada)**: A definição do agendamento que deve rodar. Contém:
    *   **Schedule**: Expressão cron (ex: `*/5 * * * *`) ou intervalos curtos (ex: `every:30m`).
    *   **HTTP Specs**: URL de destino, método HTTP (GET, POST, etc.), cabeçalhos (`Headers`) e corpo da requisição (`Payload`).
    *   **Status**: Estado atual da tarefa (`active` ou `paused`).
    *   **NextRunAt**: Timestamp exato (em UTC!) calculado para a próxima execução da tarefa.
*   **Execution (Histórico/Log de Execução)**: Registro de uma tentativa de execução de requisição HTTP pelo Worker. **É imutável após ser criada**. Contém a duração em milissegundos (`DurationMs`), o código de status HTTP retornado (`HTTPStatus`), o corpo da resposta (`ResponseBody` limitado a 2KB para evitar sobrecarga no banco de dados) e o número da tentativa de execução (`AttemptNumber`).

---

## 🔄 Fluxos de Execução Passo a Passo

### 1. Fluxo de Agendamento (O Loop do Scheduler)
O processo **Scheduler** executa um ciclo de varredura (chamado de `tick`) a cada **30 segundos**.

```mermaid
graph TD
    A[Início do Tick 30s] --> B[Adquire Lock no Redis para evitar concorrência]
    B --> C[Busca no Postgres todos os Jobs ATIVOS onde next_run_at <= NOW]
    C --> D{Encontrou Jobs?}
    D -- Não --> E[Incrementa Contador de Ciclos de Limpeza]
    D -- Sim --> F[Para cada Job elegível]
    F --> G[Calcula próximo horário next_run_at via parser]
    G --> H[Enfileira ID do Job no Redis via Asynq]
    H --> I[Atualiza next_run_at e salva no Postgres]
    I --> F
    F --> E
    E --> J{Ciclos de Limpeza >= 2880?}
    J -- Não --> K[Libera Lock e Aguarda próximo ciclo]
    J -- Sim --> L[Executa Log Garbage Collector]
    L --> M[Remove execuções mais antigas que 7 dias]
    M --> N[Zera contador de limpeza]
    N --> K
```

> [!IMPORTANT]
> **O Log Garbage Collector (Limpador de Logs)**
> Implementamos um coletor de lixo periódico direto no loop do Scheduler. Como a tabela `executions` cresce de forma agressiva (milhões de registros por dia), mantemos um contador de ciclos (`cleanupTick`).
> A cada **2880 ciclos** (aproximadamente 24 horas considerando ticks de 30s), o Scheduler executa o método `DeleteOlderThan(ctx, 7)`, removendo automaticamente todos os logs de execução mais antigos que **7 dias** para usuários do plano grátis. Isso mantém o PostgreSQL leve e performático!

---

### 2. Fluxo do Worker (Consumo e Execução)
O processo **Worker** monitora a fila do Redis de forma contínua através da biblioteca Asynq.

```
Fila Redis (Job ID)
    │
    ▼ (Consumido pelo Worker)
1. Busca detalhes completos do Job no PostgreSQL
    │
    ▼
2. Faz o disparo da requisição HTTP (utilizando pkg/httputil com timeout seguro)
    │
    ▼
3. Captura o resultado: HTTP Status, Tempo de Resposta e Corpo (truncado em 2KB)
    │
    ▼
4. Grava na tabela `executions` no PostgreSQL
    │
    ▼
5. Ocorreu erro de rede ou HTTP Status >= 500?
   ├── SIM ──► Aciona mecanismo de Retry do Asynq (Backoff Exponencial: 1min → 5min → 15min)
   │           Se falhar pela 3ª vez consecutiva:
   │           - Envia o Job para a DLQ (Dead Letter Queue)
   │           - Aciona o AlertService em background (Goroutine)
   │           - Envia POST de alerta para o Webhook cadastrado pelo usuário
   └── NÃO ──► Processo concluído com sucesso!
```

---

## 📂 Mapa do Código (Onde colocar cada arquivo?)

Para você não se perder na estrutura de diretórios, aqui está um mapa das pastas e suas regras de convivência:

```
cronflow/
├── cmd/                          ← PONTOS DE ENTRADA (Os Binários)
│   ├── api/main.go               ← Inicialização do servidor HTTP REST
│   ├── scheduler/main.go         ← Inicialização do loop do Scheduler + GC de logs
│   └── worker/main.go            ← Inicialização do pool de Workers Asynq
│
├── internal/                     ← CÓDIGO PRIVADO (Sua lógica de negócio mora aqui!)
│   ├── domain/                   ← Entidades puras (User, Project, Job, Execution).
│   │                             └── REGRA: Zero importações externas ou de outras camadas!
│   │
│   ├── repository/               ← Toda a comunicação SQL e Redis mora aqui.
│   │   ├── postgres/             ← Queries no banco (job_repository, execution_repository...)
│   │   └── redis/                ← Mecanismo de lock distribuído
│   │
│   ├── service/                  ← Regras de Negócio complexas.
│   │   ├── job_service.go        ← Validações de limites de plano, parse de cron e criações
│   │   └── alert_service.go      ← Dispara webhooks de alerta em goroutines assíncronas
│   │
│   ├── api/                      ← Camada HTTP.
│   │   ├── router/router.go      ← Definição de rotas, grupos públicos e autenticados
│   │   ├── middleware/           ← Autenticação por token e controle de abuso (Rate Limit)
│   │   └── handler/              ← Traduz JSON da API para chamadas de service (CRUDs)
│   │
│   ├── scheduler/scheduler.go    ← O loop periódico que enfileira tarefas
│   └── worker/worker.go          ← O processador que efetua os disparos HTTP
│
├── pkg/                          ← PACOTES PÚBLICOS (Ferramentas reutilizáveis)
│   ├── cronparser/               ← Interpretador e validador de expressões cron
│   ├── httputil/client.go        ← HTTP Client personalizado (timeouts e proteção de payload)
│   └── validator/                ← Validador de dados estruturados seguindo o padrão RFC 7807
│
└── migrations/                   ← VERSIONAMENTO DO BANCO DE DADOS (Arquivos SQL puros)
```

---

## 🛡️ Mecanismos de Defesa e Segurança

Como lidamos com dados sensíveis e exposição direta à internet, implementamos dois mecanismos vitais de segurança que você precisa conhecer:

### 1. Autenticação Timing-Safe com Hashing SHA-256
Os usuários acessam nossa API REST enviando uma **API Key** no cabeçalho `X-API-Key`.
*   **Armazenamento Seguro**: Nós **nunca** gravamos a API Key do usuário em texto plano no banco de dados. Salvamos apenas um hash gerado pelo algoritmo **SHA-256**.
*   **Prevenção de Timing Attacks**: Ao buscar e validar a chave recebida, utilizamos a função `subtle.ConstantTimeCompare()` em Go. Isso garante que o tempo de execução da comparação seja idêntico, independentemente de a chave fornecida estar quase correta ou completamente errada, impossibilitando hackers de deduzirem chaves analisando o tempo de resposta do servidor.

### 2. Rate Limiting via Algoritmo Janela Deslizante
Para proteger nosso banco de dados e servidores contra abusos de requisições maliciosas ou loops infinitos de clientes, aplicamos um middleware de limite de taxa utilizando o Redis.
*   **Limite**: Atualmente fixado em **60 requisições por minuto** por API Key.
*   **Funcionamento**: Cada requisição armazena um registro de timestamp em um conjunto ordenado (*Sorted Set*) no Redis. Limpezas rápidas removem timestamps antigos, e consultas contam quantos acessos ocorreram nos últimos 60 segundos. Se passar de 60, o usuário recebe um HTTP `429 Too Many Requests`.

---

## 🛠️ Como Rodar e Testar Localmente (Guia Passo a Passo)

### 1. Pré-requisitos
Certifique-se de ter instalado em sua máquina:
*   [Go 1.22 ou superior](https://go.dev/dl/)
*   [Docker e Docker Compose](https://docs.docker.com/get-docker/)

### 2. Configurando o Ambiente
Copie o template de variáveis de ambiente para criar o arquivo `.env`:
```bash
cp .env.example .env
```

### 3. Subindo a Infraestrutura
Suba os contêineres do PostgreSQL e do Redis em segundo plano:
```bash
docker compose up -d postgres redis
```

### 4. Executando as Migrations
Aplique a modelagem do banco de dados (tabelas e índices) rodando:
```bash
make migrate/up
```

### 5. Populando o Banco de Dados (Seed)
Para facilitar seus testes locais, aplique o script de seed para criar um usuário de testes e gerar chaves de API:
```bash
psql "postgres://postgres:postgres@localhost:5432/cronflow?sslmode=disable" -f scripts/seed.sql
```
*(Dica: Você também pode usar o helper `./scripts/gen_apikey.sh` para criar chaves de teste adicionais).*

### 6. Iniciando os Processos
Abra 3 terminais separados e execute cada um dos binários do MVP em modo de desenvolvimento:

*   **Terminal 1 (A API HTTP)**:
    ```bash
    make dev/api
    ```
*   **Terminal 2 (O Scheduler & Garbage Collector)**:
    ```bash
    make dev/scheduler
    ```
*   **Terminal 3 (O Worker Pool)**:
    ```bash
    make dev/worker
    ```

---

## 🧪 Exemplos Práticos de Testes (Via Curl)

Aqui estão alguns comandos prontos para você testar a API localmente a partir de sua API Key gerada no passo anterior.

### Verificar a Saúde do Sistema (Health Check)
Endpoint público para verificar se o banco de dados e o Redis estão online:
```bash
curl -i http://localhost:8080/health
```

### Criar uma Nova Tarefa Agendada (Job)
Cria um Job que realiza uma chamada POST a cada minuto para um endpoint específico:
```bash
curl -i -X POST http://localhost:8080/v1/jobs \
  -H "X-API-Key: SUA_API_KEY_DE_TESTES_GERADA" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Disparador de Webhook de Teste",
    "schedule": "*/1 * * * *",
    "timezone": "America/Sao_Paulo",
    "url": "https://httpbin.org/post",
    "http_method": "POST",
    "headers": {
      "Content-Type": "application/json",
      "User-Agent": "CronFlow-Worker/1.0"
    },
    "payload": {
      "status": "system_ok",
      "triggered_by": "scheduler"
    }
  }'
```

### Listar todos os Jobs cadastrados
```bash
curl -i http://localhost:8080/v1/jobs \
  -H "X-API-Key: SUA_API_KEY_DE_TESTES_GERADA"
```

### Consultar Logs de Execução de um Job específico
```bash
curl -i http://localhost:8080/v1/executions?job_id=ID_DO_JOB_CRIADO \
  -H "X-API-Key: SUA_API_KEY_DE_TESTES_GERADA"
```

---

## 🏆 Regras de Ouro para Desenvolvedores Juniores e Estagiários

1.  **Não coloque lógica de negócios em Handlers**: Handlers devem apenas ler parâmetros HTTP, validar o JSON de entrada com nosso validador estruturado, acionar o `Service` correto e retornar a resposta formatada. Se o seu handler tiver ifs complexos ou queries ao banco, reescreva-o enviando essa lógica para o `Service`.
2.  **Use SQL Puro e compile com SQLC**: Nós não utilizamos ORMs pesados como GORM neste projeto. Escrevemos queries SQL puras organizadas em arquivos na pasta `migrations/queries/`. Após alterar ou criar uma query, execute `make sqlc/gen` para gerar o código Go fortemente tipado automaticamente. Isso garante performance máxima e segurança em tempo de compilação!
3.  **Tudo em UTC**: O banco de dados e os cálculos do parser de cron rodam exclusivamente em UTC. Nunca use funções do sistema que peguem o fuso horário local (`time.Now()`) diretamente para salvar no banco. Sempre faça conversões usando `.UTC()`.
4.  **Atenção aos retornos do Scheduler**: No Scheduler, todos os tratamentos de erros ou situações sem jobs elegíveis passam por um fluxo que obrigatoriamente executa o nosso log de limpeza no final do método `tick()`. Nunca insira `return` precoces que pulem a instrução final de contagem e verificação de ticks.

Pronto! Agora você tem em mãos todo o conhecimento necessário para começar a codificar no CronFlow. Se tiver dúvidas, consulte os testes unitários ou converse com seu engenheiro parceiro. Bons códigos! 🚀
