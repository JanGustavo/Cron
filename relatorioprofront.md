    ──────

    ### 📂 Estrutura de Pastas do Back-end Go ( cronflow/ )

    A organização do código segue o padrão de Clean Architecture adaptado para Go, isolando lógica de domínio pura de preocupações de infraestrutura (banco de dados, HTTP):

        cronflow/                                                                                                                                                                                                      
        ├── cmd/                      # Ponto de entrada das aplicações (Server, Worker, Scheduler)                                                                                                                    
        ├── internal/                 # Código privado do projeto                                                                                                                                                      
        │   ├── api/                  # Camada HTTP (Rotas, Handlers, Middlewares)                                                                                                                                     
        │   ├── auth/                 # Lógica de criptografia, hashing de API Keys e verificação                                                                                                                      
        │   ├── config/               # Carregamento de variáveis de ambiente                                                                                                                                          
        │   ├── database/             # Conexão com o Postgres                                                                                                                                                         
        │   ├── domain/               # Entidades de Domínio e regras de negócio puras (sem dependências)                                                                                                              
        │   ├── repository/           # Camada de Persistência (Postgres com SQL puro, Redis)                                                                                                                          
        │   └── service/              # Lógica de negócio orquestrada (Validação cron, limite de plano)                                                                                                                
        ├── migrations/               # Arquivos de migração SQL (Postgres)                                                                                                                                            
        └── pkg/                      # Pacotes utilitários reutilizáveis (Cron parser, HTTP client, logs)                                                                                                             
    ──────

    ### 1. Rotas do Chi Router ( /v1 ) e Endpoints

    De acordo com a análise do arquivo router.go e dos comentários estruturais das camadas de Handlers, as rotas se dividem entre Públicas e Protegidas:

    #### 🔓 Endpoints Públicos

    Estes endpoints servem para monitoramento de infraestrutura (load balancers) e não requerem autenticação:

        •  GET /health  — Healthcheck do Postgres.
        • Response (200 OK):  {"status": "ok", "postgres": "up"}
        • Response (503 Service Unavailable):  {"status": "error", "postgres": "down"}

    #### 🔒 Endpoints Protegidos (Grupo  /v1 )

    Todos os endpoints abaixo passam pelo middleware  middleware.Auth  e exigem o header de autenticação:
    Authorization: Bearer cf_live_<token_32_bytes_hex>

    • Jobs:
        •  GET /v1/jobs  — Retorna todos os jobs associados ao projeto autenticado (ordenados por criação decrescente).
        •  POST /v1/jobs  — Cria um novo job. Executa a validação da expressão cron, calcula o próximo tempo de execução ( next_run_at ) e aplica limites do plano do usuário.
        •  GET /v1/jobs/{id}  — Retorna os detalhes de um job específico.
            •  PATCH /v1/jobs/{id}  — Atualiza o status do job ( active | paused ).
        •  DELETE /v1/jobs/{id}  — Remove o job (com verificação estrita de multi-tenancy no banco de dados com base no  project_id ).
    • Logs / Executions:
            •  (Ainda não implementado no router atual)

    ──────

    ### 2. Handlers HTTP e Mecanismo de Autenticação

    Os HTTP Handlers em Go são propositalmente "thin" (finos). Eles apenas deserializam os requests vindos do cliente, passam a execução para a camada de serviços ( Service ) e serializam o retorno.

    #### Middleware de Autenticação ( internal/api/middleware/auth.go )

    • Regra de Criptografia: As chaves brutas de API (plain-text) geradas seguem o prefixo  cf_live_  concatenado com 32 bytes em hex aleatórios. Nenhum plain-text é salvo no banco. O banco armazena apenas o
    hash  SHA-256  gerado por  auth.Hash() .
    • Verificação: O middleware calcula o hash e faz lookup no banco. A função  auth.Verify  (com  subtle.ConstantTimeCompare ) existe, mas não é usada no middleware atual.
    • Injeção de Contexto: Se a API Key for válida, o middleware injeta a struct  Project  correspondente no  context.Context  da requisição HTTP. Os Handlers podem extrair o projeto em tempo de execução de forma
    segura.
    • Respostas 401 distintas:
        • Header ausente/inválido → {"error":"unauthorized","reason":"Authorization header ausente ou inválido"}
        • API Key inválida → {"error":"unauthorized","reason":"API Key inválida"}
    ──────

    ### 3. Modelos de Banco, Structs Go e Contratos TypeScript

    Aqui está o mapeamento exato entre as migrações Postgres ( migrations/ ), as structs do domínio em Go ( internal/domain/ ) e o equivalente ideal em TypeScript para as stores do Zustand e chamadas do React
    Query.

    #### A. Entidade  Project  (Workspace)

    Representa um agrupamento lógico de jobs. Um usuário pode ter múltiplos projetos.

    • Schema do Postgres:
        CREATE TABLE projects (
            id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
            name       TEXT NOT NULL,
            created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
        );

    • Struct Go ( domain/project/project.go ):
        type Project struct {
            ID        string
            UserID    string
            Name      string
            CreatedAt time.Time
        }

    • Contrato TypeScript:
        export interface Project {
        id: string;
        userId: string;
        name: string;
        createdAt: string; // ISO 8601 string
        }

    #### B. Entidade  Job

    Define as tarefas recorrentes que executam requisições HTTP (Webhooks).

    • Schema do Postgres:
        CREATE TABLE jobs (
            id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            project_id           UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
            name                 TEXT NOT NULL,
            schedule             TEXT NOT NULL,         -- Expressão cron (ex: "*/5* ** *") ou intervalo ("every:15m")
            timezone             TEXT NOT NULL DEFAULT 'UTC',
            url                  TEXT NOT NULL,
            http_method          TEXT NOT NULL DEFAULT 'POST',
            headers              JSONB,                 -- map[string]string serializado
            payload              JSONB,                 -- Payload HTTP serializado
            status               TEXT NOT NULL DEFAULT 'active', -- active | paused | failing
            next_run_at          TIMESTAMPTZ NOT NULL,
            last_run_at          TIMESTAMPTZ,
            consecutive_failures INT NOT NULL DEFAULT 0,
            webhook_alert_url    TEXT,                  -- URL de fallback em caso de erros persistentes
            created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
            updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
        );

    • Struct Go ( domain/job/job.go ):
        type Status string
        const (
            StatusActive  Status = "active"
            StatusPaused  Status = "paused"
            StatusFailing Status = "failing"
        )

        type HTTPMethod string                                                                                                                                                                                         
        const (                                                                                                                                                                                                        
            MethodGet  HTTPMethod = "GET"                                                                                                                                                                                 
            MethodPost HTTPMethod = "POST"                                                                                                                                                                                
        )                                                                                                                                                                                                              
                                                                                                                                                                                                                    
        type Job struct {                                                                                                                                                                                              
        ID                  string                                                                                                                                                                                   
        ProjectID           string                                                                                                                                                                                   
        Name                string                                                                                                                                                                                   
        Schedule            string                                                                                                                                                                                   
        Timezone            string                                                                                                                                                                                   
        URL                 string                                                                                                                                                                                    
        HTTPMethod          HTTPMethod                                                                                                                                                                                
        Headers             map[string]string                                                                                                                                                                         
        Payload             map[string]any                                                                                                                                                                            
        Status              Status                                                                                                                                                                                    
        NextRunAt           time.Time                                                                                                                                                                                 
        LastRunAt           *time.Time // Nullable                                                                                                                                                                     
        ConsecutiveFailures int                                                                                                                                                                                       
        WebhookAlertURL     *string // Nullable                                                                                                                                                                        
        CreatedAt           time.Time                                                                                                                                                                                 
        UpdatedAt           time.Time                                                                                                                                                                                 
        }                                                                                                                                                                                                              
                                                                                                                                                                                                                    
    • Contrato TypeScript:
        export type JobStatus = 'active' | 'paused' | 'failing';
        export type HTTPMethod = 'GET' | 'POST';

        export interface Job {                                                                                                                                                                                         
        id: string;                                                                                                                                                                                                  
        projectId: string;                                                                                                                                                                                           
        name: string;                                                                                                                                                                                                
        schedule: string; // ex: "0 9 * * 1" ou "every:15m"                                                                                                                                                          
        timezone: string; // Padrão "UTC"                                                                                                                                                                            
        url: string;                                                                                                                                                                                                 
        httpMethod: HTTPMethod;                                                                                                                                                                                      
        headers: Record<string, string> | null; // map[string]string                                                                                                                                                  
        payload: Record<string, unknown> | null;                                                                                                                                                                      
        status: JobStatus;                                                                                                                                                                                           
        nextRunAt: string; // ISO 8601                                                                                                                                                                               
        lastRunAt: string | null; // Nullable                                                                                                                                                                        
        consecutiveFailures: number;                                                                                                                                                                                 
        webhookAlertUrl: string | null; // Nullable                                                                                                                                                                  
        createdAt: string;                                                                                                                                                                                           
        updatedAt: string;                                                                                                                                                                                           
        }                                                                                                                                                                                                              
                                                                                                                                                                                                                    
                                                                                                                                                                                                                    
    #### C. Entidade  Execution  (Logs)

    Representa cada tentativa realizada pelo Worker de chamar a URL alvo do Job.

    • Schema do Postgres:
        CREATE TABLE executions (
            id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            job_id         UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
            triggered_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
            started_at     TIMESTAMPTZ,
            finished_at    TIMESTAMPTZ,
            status         TEXT NOT NULL,   -- success | failed | timeout
            http_status    INT,
            duration_ms    INT,
            response_body  TEXT,            -- Guardado no banco limitado a 2KB
            attempt_number INT NOT NULL DEFAULT 1
        );

    • Struct Go ( domain/execution/execution.go ):
        type Status string
        const (
            StatusSuccess Status = "success"
            StatusFailed  Status = "failed"
            StatusTimeout Status = "timeout"
        )

        type Execution struct {                                                                                                                                                                                        
            ID            string     `json:"id"`                                                                                                                                                                          
            JobID         string     `json:"job_id"`                                                                                                                                                                      
            Status        Status     `json:"status"`                                                                                                                                                                      
            HTTPStatus    *int       `json:"http_status"` // Nullable se houver falha de rede/DNS                                                                                                                         
            DurationMs    int        `json:"duration_ms"`                                                                                                                                                                 
            ResponseBody  string     `json:"response_body"`                                                                                                                                                               
            AttemptNumber int        `json:"attempt_number"`                                                                                                                                                              
            TriggeredAt   time.Time  `json:"triggered_at"`                                                                                                                                                                
            StartedAt     *time.Time `json:"started_at"`  // Nullable                                                                                                                                                     
            FinishedAt    *time.Time `json:"finished_at"` // Nullable                                                                                                                                                     
        }                                                                                                                                                                                                              
                                                                                                                                                                                                                    
    • Contrato TypeScript:
        export type ExecutionStatus = 'success' | 'failed' | 'timeout';

        export interface Execution {                                                                                                                                                                                   
        id: string;                                                                                                                                                                                                  
        jobId: string;                                                                                                                                                                                               
        status: ExecutionStatus;                                                                                                                                                                                     
        httpStatus: number | null; // Nullable se timeout/erro de DNS                                                                                                                                                
        durationMs: number;                                                                                                                                                                                          
        responseBody: string; // Max 2KB                                                                                                                                                                             
        attemptNumber: number;                                                                                                                                                                                       
        triggeredAt: string;                                                                                                                                                                                         
        startedAt: string | null;                                                                                                                                                                                    
        finishedAt: string | null;                                                                                                                                                                                   
        }                                                                                                                                                                                                              
                                                                                                                                                                                                                    
    ──────

    ### 4. Envelope de Erro Padronizado

    O back-end atual retorna um envelope simples de erro:

    • Erros de validação (400): {"error":"<mensagem>"}
    • Limite de plano (403): {"error":"limite de jobs do plano atingido - faca upgrade"}
    • Não encontrado (404): {"error":"job nao encontrado"}
    • Sem autorização (401): {"error":"unauthorized","reason":"..."}

    ──────

    ### 5. Alinhamento com as Stores do Zustand e React Query

    Com base nos contratos descobertos, o alinhamento das stores do front-end deve contemplar:

    1. Gestão de API Key / Autenticação ( authStore.ts ):
        • O token digitado pelo usuário deve ser persistido (ex:  localStorage ) e enviado no header  Authorization: Bearer <key> .
        • Um interceptor global do Axios (ou cabeçalho dinâmico no  fetch ) deve ler essa store antes de disparar qualquer chamada.
    2. React Query para Dados Dinâmicos ( jobs  e  executions ):
        • As buscas de listagem de jobs ( GET /v1/jobs ) e logs ( GET /v1/jobs/{id}/executions ) devem ser migradas do Zustand para consultas do React Query ( useQuery ). Isso garante cache inteligente,
        invalidação automática após criação/deleção e revalidação em segundo plano.
        • O Zustand deve reter estados globais de UI (como filtro de busca atual do kanban, modal ativo, tema do dashboard ou job selecionado no momento).
        3. Mapeamento de PascalCase para camelCase:
        • O backend em Go (structs sem json tags) retorna chaves em PascalCase (ex:  ProjectID ,  NextRunAt ).
        • No front-end React, mapear para camelCase na camada de serviço da API (projectId, nextRunAt).

    Com este mapeamento estático completo, o ecossistema do front-end está 100% munido com as assinaturas reais da API para implementar as integrações de rede.  
