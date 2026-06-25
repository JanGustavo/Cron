# 🚀 CronFlow — Plataforma de Agendamento e Automação de Tarefas

## 🌐 Link de Produção
O sistema está implantado e pronto para uso em: **[https://cron.jangustavo.me/](https://cron.jangustavo.me/)**

A interface web (frontend) e os serviços de processamento (backend) estão organizados em repositórios separados no GitHub:
- **Repositório do Backend (Este Repo)**: [github.com/JanGustavo/Cron](https://github.com/JanGustavo/Cron)
- **Repositório do Frontend**: [github.com/JanGustavo/Cron-interface](https://github.com/JanGustavo/Cron-interface)

---

## 📝 Sobre o CronFlow
O **CronFlow** é uma plataforma SaaS (Software as a Service) de agendamento de tarefas e disparo de webhooks ("CronTab em escala como serviço"). O sistema permite que nossos usuários cadastrem requisições HTTP agendadas (via expressões cron) que devem ser executadas com alta precisão, tolerância a falhas e total rastreabilidade.

---

## 🗺️ Visão Geral da Arquitetura (Múltiplos Binários)

Em vez de construirmos um monólito gigante e pesado, o CronFlow foi projetado seguindo o **Princípio da Responsabilidade Única (SRP)** no nível de infraestrutura. O sistema é dividido em **3 processos totalmente independentes** (binários distintos em Go) que rodam lado a lado no backend, comunicando-se através de duas fontes: o banco de dados **PostgreSQL** e o broker de mensagens **Redis**. Uma interface **React** consome a API REST principal.

```
                  ┌─────────────────────────────────────────────────────────────┐
                  │                      CLIENTE (Nginx)                        │
                  └──────────────┬───────────────────────────────┬──────────────┘
                                 │ HTTPS (Web App)               │ REST API
                                 ▼                               ▼
                  ┌──────────────────────────────┐ ┌────────────────────────────┐
                  │   FRONTEND: Cron-interface   │ │   PROCESSO 1: API REST     │
                  │      (React + Vite)          │ │     (cronflow/cmd/api)     │
                  │      TailwindCSS v4          │ │    Autenticação SHA-256    │
                  └──────────────────────────────┘ └──────┬──────────────┬──────┘
                                                          │              │
                                          Fonte da Verdade│              │ Enfileira Job
                                                          ▼              ▼
                                                ┌────────────┐ ┌────────────────┐
                                                │ PostgreSQL │ │ Redis & Asynq  │
                                                │     16     │ │ Fila de Jobs   │
                                                └─────▲──────┘ └──────▲─────────┘
                                                      │               │
                                         Leitura      │               │ Consome Job
                                      ┌───────────────┴──┐            │ (Goroutines)
                                      │    PROCESSO 2:   │     ┌──────▼─────────┐
                                      │     SCHEDULER    ├────►│   PROCESSO 3:  │
                                      │(cmd/scheduler)   │     │     WORKER     │
                                      │Loop 30s · Lock   │     │  (cmd/worker)  │
                                      └──────────────────┘     └────────────────┘
```

### Por que separamos em 3 processos no backend?
1. **API (`cmd/api`)**: Precisa de alta escalabilidade e baixa latência. Pode ser multiplicada em várias réplicas conforme a quantidade de requisições de clientes crescer.
2. **Scheduler (`cmd/scheduler`)**: É o coração do agendamento. Para evitar execuções duplicadas, **só pode haver exatamente 1 instância ativa** rodando (usamos locks distribuídos no Redis para garantir isso).
3. **Worker (`cmd/worker`)**: É totalmente *stateless* (não guarda estado). Ele pode ser escalado horizontalmente de forma agressiva para aguentar rajadas massivas de execuções HTTP sem afetar a performance da API.

---

## 🗂️ Glossário de Domínio (As Entidades)

Estas são as principais entidades de negócio do CronFlow mapeadas em `internal/domain/`:

*   **User (Usuário)**: A conta principal do cliente cadastrado. Controla qual **plano** de assinatura (ex: `free` ou `paid`) está ativo. Não usamos senhas; a autenticação na API é feita exclusivamente via **API Key**.
*   **Project (Projeto/Workspace)**: O ambiente (tenant) isolado de trabalho de um usuário. Um usuário pode criar múltiplos projetos para separar seus ambientes (ex: `produção`, `homologação`).
*   **Job (Tarefa Agendada)**: A definição do agendamento que deve rodar. Contém:
    *   **Schedule**: Expressão cron (ex: `*/5 * * * *`) ou intervalos curtos (ex: `every:30m`).
    *   **HTTP Specs**: URL de destino, método HTTP (GET, POST, etc.), cabeçalhos (`Headers`) e corpo da requisição (`Payload`).
    *   **Status**: Estado atual da tarefa (`active`, `paused` ou `failing`).
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

## 💻 Frontend (Interface do Usuário)

O frontend (`Cron-interface`) foi desenvolvido em React para fornecer uma experiência rica e reativa:

*   **Dashboard Executivo**:
    *   Gráficos dinâmicos com **Recharts** que exibem o volume de requisições disparadas e a taxa de sucesso.
    *   Filtros rápidos de período (`1h`, `24h`, `3d`, `7d`, `30d`) e seleção por jobs específicos.
    *   Painel de estatísticas rápidas com contagem total de jobs ativos, pausados e em falha.
    *   Feed de atividades recentes com as últimas execuções de requisições.
*   **Quadro Kanban Interativo**:
    *   Visualização clara do status de cada job (Ativo, Pausado, Falhando).
    *   Fluxo de arrastar e soltar (Drag and Drop) integrado para pausar ou reativar tarefas instantaneamente.
    *   Modais detalhados para visualização de parâmetros e criação de novos Jobs (com suporte a customização de payload JSON, headers HTTP e timezone).
*   **Histórico de Logs Completo**:
    *   Visualização de tabelas e códigos de status retornados, tempo de resposta e corpo de retorno de até 2KB de cada disparo.
*   **Gerenciamento de API Key e Webhooks**:
    *   Na página de perfil, o usuário pode copiar a sua API Key secreta e configurar uma URL de webhook de alerta global para receber notificações imediatas de erros do Worker.

---

## 📂 Mapa do Código do Backend

Para você não se perder na estrutura de diretórios do backend (`cronflow/`):

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

Como lidamos com dados sensíveis e exposição direta à internet, implementamos dois mecanismos vitais de segurança:

### 1. Autenticação Timing-Safe com Hashing SHA-256
Os usuários acessam nossa API REST enviando uma **API Key** no cabeçalho `X-API-Key`.
*   **Armazenamento Seguro**: Nós **nunca** gravamos a API Key do usuário em texto plano no banco de dados. Salvamos apenas um hash gerado pelo algoritmo **SHA-256**.
*   **Prevenção de Timing Attacks**: Ao buscar e validar a chave recebida, utilizamos a função `subtle.ConstantTimeCompare()` em Go. Isso garante que o tempo de execução da comparação seja idêntico, independentemente de a chave fornecida estar quase correta ou completamente errada.

### 2. Rate Limiting via Algoritmo Janela Deslizante
Para proteger nosso banco de dados e servidores contra abusos de requisições ou loops infinitos de clientes, aplicamos um middleware de limite de taxa utilizando o Redis.
*   **Limite**: Atualmente fixado em **60 requisições por minuto** por API Key.
*   **Funcionamento**: Cada requisição armazena um registro de timestamp em um conjunto ordenado (*Sorted Set*) no Redis. Limpezas rápidas removem timestamps antigos, e consultas contam quantos acessos ocorreram nos últimos 60 segundos. Se passar de 60, o usuário recebe um HTTP `429 Too Many Requests`.

---

## 🛠️ Como Rodar e Testar Localmente (Guia Passo a Passo)

### 1. Pré-requisitos
Certifique-se de ter instalado em sua máquina:
*   [Go 1.22 ou superior](https://go.dev/dl/)
*   [Node.js 18 ou superior](https://nodejs.org/)
*   [Docker e Docker Compose](https://docs.docker.com/get-docker/)

### 2. Iniciando a Infraestrutura e o Backend
1. Entre na pasta do backend e crie o arquivo `.env`:
   ```bash
   cd cronflow
   cp .env.example .env
   ```
2. Suba os contêineres do PostgreSQL e do Redis em segundo plano:
   ```bash
   docker compose up -d postgres redis
   ```
3. Aplique as migrations de tabelas do banco de dados:
   ```bash
   make migrate/up
   ```
4. Adicione o banco de dados inicial (Seed) para criar o usuário e a API Key padrão de testes:
   ```bash
   psql "postgres://postgres:postgres@localhost:5432/cronflow?sslmode=disable" -f scripts/seed.sql
   ```
5. Abra 3 terminais separados e execute os microsserviços do backend:
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

### 3. Iniciando a Interface do Usuário (Frontend)
1. Acesse o diretório do frontend:
   ```bash
   cd "../cron front"
   ```
2. Instale as dependências:
   ```bash
   npm install
   ```
3. Configure a URL da API local criando um arquivo `.env.local`:
   ```env
   VITE_API_URL=http://localhost:8080
   ```
4. Execute o servidor de desenvolvimento:
   ```bash
   npm run dev
   ```
   *O painel estará acessível em [http://localhost:5173/](http://localhost:5173/)*

---

## 🧪 Exemplos Práticos de Testes (Via Curl)

Substitua `SUA_API_KEY_DE_TESTES_GERADA` pela chave criada no passo do script de seed.

### Verificar a Saúde do Backend (Health Check)
```bash
curl -i http://localhost:8080/health
```

### Criar um Novo Job Agendado
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

## 🏆 Diretrizes de Desenvolvimento e Boas Práticas

1.  **Não coloque lógica de negócios em Handlers**: Handlers devem apenas ler parâmetros HTTP, validar o JSON de entrada, acionar o `Service` correto e retornar a resposta formatada.
2.  **Use SQL Puro e compile com SQLC**: Escrevemos queries SQL puras organizadas em arquivos na pasta `migrations/queries/`. Após alterar ou criar uma query, execute `make sqlc/gen` para gerar o código Go tipado.
3.  **Tudo em UTC**: O banco de dados e os cálculos de cron rodam em UTC. Nunca use funções do sistema que peguem o fuso horário local (`time.Now()`) diretamente para salvar no banco. Sempre faça conversões usando `.UTC()`.
4.  **Atenção aos retornos do Scheduler**: No Scheduler, todos os tratamentos de erros ou situações sem jobs elegíveis passam por um fluxo que obrigatoriamente executa o nosso log de limpeza no final do método `tick()`. Nunca insira `return` precoces que pulem a instrução final de contagem e verificação de ticks.
5.  **Mantenha as Convenções de Nomes**: O frontend faz chamadas convertendo objetos JSON em `camelCase`. Certifique-se de que novos atributos no backend estejam devidamente mapeados nos conversores do Axios (`cron front/src/services/api.ts`) se adicionadas siglas não usuais.
