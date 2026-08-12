package worker_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/joho/godotenv"

	"github.com/JanGustavo/Cron/internal/database"
	"github.com/JanGustavo/Cron/internal/domain/job"
	"github.com/JanGustavo/Cron/internal/queue"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/service"
	"github.com/JanGustavo/Cron/internal/worker"
	_ "github.com/lib/pq"
)

func TestWorkerIntegration(t *testing.T) {
	// Permite conexões locais para o servidor de teste (ignora firewall SSRF localmente)
	os.Setenv("ALLOW_LOCAL_WEBHOOKS", "true")

	// Carrega as variáveis do arquivo .env do projeto para conectar no banco real local
	_ = godotenv.Load("../../.env")

	// 1. Configura URLs de teste (postgres e redis)
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/cronflow?sslmode=disable"
	}
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6379"
	}

	// Tenta conectar ao Postgres
	db, err := database.Connect(dbURL)
	if err != nil {
		t.Skipf("Pulando teste de integração: erro ao conectar no Postgres: %v", err)
		return
	}
	defer db.Close()

	// Verifica se a tabela users/jobs existe realizando um ping rápido
	if err := db.Ping(); err != nil {
		t.Skipf("Pulando teste de integração: ping no Postgres falhou: %v", err)
		return
	}

	// Garante DDL do CPF, Nome Completo, WebhookSecret e LastUsedAt no banco de teste
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS cpf VARCHAR(11) UNIQUE; ALTER TABLE users ADD COLUMN IF NOT EXISTS full_name TEXT; ALTER TABLE projects ADD COLUMN IF NOT EXISTS webhook_secret TEXT; ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ;`)

	// 2. Cria um servidor HTTP mock (Webhook de Teste)
	webhookCalled := false
	var receivedMethod string
	var receivedBody string

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookCalled = true
		receivedMethod = r.Method
		bodyBytes, _ := io.ReadAll(r.Body)
		receivedBody = string(bodyBytes)
		
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"received"}`))
	}))
	defer mockServer.Close()

	// 3. Configura os Repositories e Services
	jobRepo := postgres.NewJobRepository(db)
	execRepo := postgres.NewExecutionRepository(db)
	userRepo := postgres.NewUserRepository(db)
	mailService := service.NewMailService("", 587, "", "", "")
	alertService := service.NewAlertService(db, mailService)

	// Inicializa enqueuer
	enqueuer := queue.NewEnqueuer(redisURL)
	defer enqueuer.Close()

	ctx := context.Background()

	// Cria um usuário e projeto temporário para o teste se não houver registros
	// Para não quebrar o banco, vamos buscar se já existe o admin@cronflow.sh
	u, err := userRepo.FindByEmail(ctx, "admin@cronflow.sh")
	if err != nil {
		t.Fatalf("Erro ao buscar usuário administrador de teste: %v", err)
	}
	if u == nil {
		// Se não existir, cria um
		u, err = userRepo.CreateUserWithPassword(ctx, "admin@cronflow.sh", "test-pwd-hash", "Admin Tester", "11144477735")
		if err != nil {
			t.Fatalf("Erro ao criar usuário de teste: %v", err)
		}
	}

	projs, err := userRepo.FindProjectsByUserID(ctx, u.ID)
	if err != nil {
		t.Fatalf("Erro ao buscar projetos de teste: %v", err)
	}
	var projectID string
	if len(projs) == 0 {
		proj, err := userRepo.CreateProject(ctx, u.ID, "Projeto de Teste")
		if err != nil {
			t.Fatalf("Erro ao criar projeto de teste: %v", err)
		}
		projectID = proj.ID
	} else {
		projectID = projs[0].ID
	}

	// 4. Cria o Job de teste vinculando ao mockServer
	testJob := &job.Job{
		ProjectID:  projectID,
		Name:       "Job de Teste de Integração",
		Schedule:   "*/5 * * * *",
		HTTPMethod: job.MethodPost,
		URL:        mockServer.URL + "/webhook",
		Timezone:   "America/Sao_Paulo",
		Headers:    map[string]string{"X-Test-Header": "CronFlowTest"},
		Payload:    map[string]interface{}{"event": "integration_test"},
		Status:     job.StatusActive,
	}

	_, err = jobRepo.Create(ctx, testJob)
	if err != nil {
		t.Fatalf("Erro ao cadastrar Job de teste: %v", err)
	}
	// Garante remoção do Job de teste após execução
	defer func() {
		_ = jobRepo.Delete(ctx, testJob.ID, testJob.ProjectID)
	}()

	// 5. Enfileira a execução usando o Enqueuer
	err = enqueuer.Enqueue(ctx, testJob.ID)
	if err != nil {
		t.Fatalf("Erro ao enfileirar Job no Redis: %v", err)
	}

	// 6. Instancia o Worker e processa a task manualmente para isolar o teste
	w := worker.New(jobRepo, execRepo, alertService, enqueuer, "test-jwt-secret")

	// Cria a task do Asynq manualmente simulando o consumo da fila
	payloadBytes, _ := json.Marshal(queue.HTTPJobPayload{JobID: testJob.ID})
	task := asynq.NewTask(queue.TypeHTTPJob, payloadBytes)

	// Executa o processador do Worker
	err = w.ProcessTask(ctx, task)
	if err != nil {
		t.Fatalf("ProcessTask retornou erro inesperado: %v", err)
	}

	// 7. Validações
	// A. Verifica se o Webhook mock foi invocado
	if !webhookCalled {
		t.Error("Esperava que o mock HTTP server (webhook) fosse chamado, mas não foi")
	}
	if receivedMethod != "POST" {
		t.Errorf("Método esperado POST, recebido: %s", receivedMethod)
	}
	if !strings.Contains(receivedBody, "integration_test") {
		t.Errorf("Esperava payload contendo 'integration_test', recebido: %s", receivedBody)
	}

	// B. Verifica se a execução (Execution log) foi persistida no banco
	execs, err := execRepo.ListByJob(ctx, testJob.ID, 10)
	if err != nil {
		t.Fatalf("Erro ao listar execuções salvas: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("Esperava exatamente 1 log de execução no banco, encontrado: %d", len(execs))
	}

	savedExec := execs[0]
	if savedExec.Status != "success" {
		t.Errorf("Status esperado 'success', recebido: %s", savedExec.Status)
	}
	if savedExec.HTTPStatus == nil || *savedExec.HTTPStatus != 200 {
		t.Errorf("HTTPStatus esperado 200, recebido: %v", savedExec.HTTPStatus)
	}
	if savedExec.DurationMs < 0 {
		t.Errorf("DurationMs esperada maior ou igual a 0, recebido: %d", savedExec.DurationMs)
	}
	if !strings.Contains(savedExec.ResponseBody, "received") {
		t.Errorf("ResponseBody esperado contendo 'received', recebido: %s", savedExec.ResponseBody)
	}
}
