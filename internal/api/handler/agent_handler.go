package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/JanGustavo/Cron/internal/api/middleware"
	"github.com/JanGustavo/Cron/internal/config"
	"github.com/JanGustavo/Cron/internal/domain/job"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/service"
	"github.com/JanGustavo/Cron/pkg/httputil"
	"github.com/redis/go-redis/v9"
)

type contextKey string
const (
	userIDKey   contextKey = "userID"
	clientIPKey contextKey = "clientIP"
)

func getClientIP(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.Header.Get("X-Real-IP")
	}
	if ip == "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			ip = host
		} else {
			ip = r.RemoteAddr
		}
	}
	if strings.Contains(ip, ",") {
		parts := strings.Split(ip, ",")
		return strings.TrimSpace(parts[0])
	}
	return ip
}

type PendingOperation struct {
	Tool      string
	ArgsHash  string
	ProjectID string
	ExpiresAt time.Time
}

type AgentHandler struct {
	jobService *service.JobService
	userRepo   *postgres.UserRepository
	cfg        *config.Config
	pendingOps map[string]PendingOperation
	pendingMu  sync.RWMutex
	redis      *redis.Client
}

func NewAgentHandler(jobService *service.JobService, userRepo *postgres.UserRepository, cfg *config.Config) *AgentHandler {
	var rClient *redis.Client
	if cfg.RedisURL != "" {
		opt, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			rClient = redis.NewClient(&redis.Options{
				Addr: cfg.RedisURL,
			})
		} else {
			rClient = redis.NewClient(opt)
		}
	}
	return &AgentHandler{
		jobService: jobService,
		userRepo:   userRepo,
		cfg:        cfg,
		pendingOps: make(map[string]PendingOperation),
		redis:      rClient,
	}
}

type AgentChatRequest struct {
	Message string          `json:"message" example:"crie um job para rodar a cada 5 minutos batendo em https://httpbin.org/get"`
	History []GeminiMessage `json:"history"`
}

type AgentChatResponseError struct {
	Code      string `json:"code"`
	Retryable bool   `json:"retryable"`
}

type AgentChatResponse struct {
	Reply             string                  `json:"reply" example:"Por favor, confirme a criação do job com os seguintes parâmetros: ... Código de confirmação: CF-9B3D"`
	History           []GeminiMessage         `json:"history"`
	Status            string                  `json:"status,omitempty" example:"CONFIRMATION_REQUIRED"`
	ConfirmationToken string                  `json:"confirmationToken,omitempty" example:"CF-9B3D"`
	Error             *AgentChatResponseError `json:"error,omitempty"`
}

// Gemini Types
type GeminiMessage struct {
	Role  string       `json:"role"`
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *GeminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *GeminiFunctionResponse `json:"functionResponse,omitempty"`
	ThoughtSignature string                  `json:"thoughtSignature,omitempty"`
}

type GeminiFunctionCall struct {
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type GeminiFunctionResponse struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}
type GeminiGenerationConfig struct {
	Temperature     float32 `json:"temperature,omitempty"`
	TopP            float32 `json:"topP,omitempty"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
}

type GeminiRequest struct {
	Contents          []GeminiMessage          `json:"contents"`
	SystemInstruction *GeminiSystemInstruction `json:"systemInstruction,omitempty"`
	Tools             []GeminiTool             `json:"tools,omitempty"`
	GenerationConfig  *GeminiGenerationConfig  `json:"generationConfig,omitempty"`
}

type GeminiSystemInstruction struct {
	Parts []GeminiPart `json:"parts"`
}

type GeminiTool struct {
	FunctionDeclarations []GeminiFunctionDeclaration `json:"functionDeclarations"`
}

type GeminiFunctionDeclaration struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Parameters  GeminiSchema `json:"parameters"`
}

type GeminiSchema struct {
	Type        string                  `json:"type"`
	Properties  map[string]GeminiSchema `json:"properties,omitempty"`
	Required    []string                `json:"required,omitempty"`
	Description string                  `json:"description,omitempty"`
}

type GeminiResponse struct {
	Candidates []GeminiCandidate `json:"candidates"`
}

type GeminiCandidate struct {
	Content GeminiMessage `json:"content"`
}

// OpenAI/Groq Fallback Types
type OpenAIMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content,omitempty"`
	ToolCalls  []OpenAIToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}

type OpenAIToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function OpenAIFunction   `json:"function"`
}

type OpenAIFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type OpenAISchema struct {
	Type        string                  `json:"type"`
	Properties  map[string]OpenAISchema `json:"properties,omitempty"`
	Required    []string                `json:"required,omitempty"`
	Description string                  `json:"description,omitempty"`
}

type OpenAITool struct {
	Type     string             `json:"type"`
	Function OpenAIFunctionDecl `json:"function"`
}

type OpenAIFunctionDecl struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Parameters  OpenAISchema `json:"parameters"`
}

type OpenAIRequest struct {
	Model       string          `json:"model"`
	Messages    []OpenAIMessage `json:"messages"`
	Tools       []OpenAITool    `json:"tools,omitempty"`
	Temperature float32         `json:"temperature,omitempty"`
	TopP        float32         `json:"top_p,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
}

type OpenAIResponse struct {
	Choices []OpenAIChoice `json:"choices"`
}

type OpenAIChoice struct {
	Message OpenAIMessage `json:"message"`
}

var geminiTools = []GeminiTool{
	{
		FunctionDeclarations: []GeminiFunctionDeclaration{
			{
				Name:        "listJobs",
				Description: "Lista todas as tarefas agendadas (Cron Jobs) ativas ou cadastradas no sistema CronFlow.",
				Parameters: GeminiSchema{
					Type: "OBJECT",
				},
			},
			{
				Name:        "createJob",
				Description: "Cria uma nova tarefa agendada (Cron Job) no CronFlow a partir de parâmetros como nome, URL de destino e agendamento.",
				Parameters: GeminiSchema{
					Type: "OBJECT",
					Properties: map[string]GeminiSchema{
						"name": {
							Type:        "STRING",
							Description: "Nome legível/amigável da tarefa (ex: Sincronização de Pedidos)",
						},
						"schedule": {
							Type:        "STRING",
							Description: "Agendamento cron padrão (ex: '*/5 * * * *') ou intervalo simplificado (ex: 'every:1h', 'every:30m'). Traduza pedidos informais do usuário para o formato de agendamento do CronFlow.",
						},
						"url": {
							Type:        "STRING",
							Description: "URL HTTP de destino (webhook) que receberá o disparo agendado (ex: https://meu-app.com/api/webhook)",
						},
						"httpMethod": {
							Type:        "STRING",
							Description: "Método HTTP a ser usado no disparo (GET, POST, PUT, DELETE, PATCH). O valor deve ser explicitamente fornecido.",
						},
						"headers": {
							Type:        "STRING",
							Description: "String JSON válida representando headers HTTP opcionais (ex: '{\"Content-Type\": \"application/json\", \"Authorization\": \"Bearer key\"}')",
						},
						"payload": {
							Type:        "STRING",
							Description: "Objeto JSON válido (ex: '{\"key\": \"value\"}') que será enviado como corpo da requisição para métodos POST/PUT. Deve ser obrigatoriamente um objeto JSON válido, strings simples não estruturadas não são aceitas.",
						},
						"webhookAlertUrl": {
							Type:        "STRING",
							Description: "URL opcional para receber alertas, conforme a política de alertas configurada.",
						},
						"confirmationToken": {
							Type:        "STRING",
							Description: "O código de confirmação no formato 'CF-XXXXXX'. Quando o usuário disser 'confirmo', 'sim' ou fornecer o código, passe o código para efetivar a ação.",
						},
					},
					Required: []string{"name", "schedule", "url", "httpMethod"},
				},
			},
			{
				Name:        "triggerJob",
				Description: "Dispara a execução imediata e manual de um Job específico cadastrado utilizando seu ID único.",
				Parameters: GeminiSchema{
					Type: "OBJECT",
					Properties: map[string]GeminiSchema{
						"jobId": {
							Type:        "STRING",
							Description: "O ID da tarefa (formato UUID) obtido na listagem de jobs.",
						},
						"confirmationToken": {
							Type:        "STRING",
							Description: "O código de confirmação no formato 'CF-XXXXXX'. Quando o usuário disser 'confirmo', 'sim' ou fornecer o código, passe o código para efetivar a ação.",
						},
					},
					Required: []string{"jobId"},
				},
			},
			{
				Name:        "executeCurlDirect",
				Description: "Executa uma requisição HTTP cURL/HTTP imediata e em tempo real, retornando o resultado (status, tempo e corpo da resposta) sem salvar no banco de dados.",
				Parameters: GeminiSchema{
					Type: "OBJECT",
					Properties: map[string]GeminiSchema{
						"url": {
							Type:        "STRING",
							Description: "URL de destino para a requisição.",
						},
						"method": {
							Type:        "STRING",
							Description: "Método HTTP (GET, POST, PUT, DELETE, etc.). Padrão é GET.",
						},
						"headers": {
							Type:        "STRING",
							Description: "JSON contendo cabeçalhos HTTP adicionais (ex: '{\"Authorization\": \"Bearer X\"}').",
						},
						"payload": {
							Type:        "STRING",
							Description: "Corpo da requisição HTTP (JSON ou texto bruto) para métodos POST/PUT.",
						},
					},
					Required: []string{"url"},
				},
			},
		},
	},
}

const systemInstruction = `Você é o CronFlow AI Agent, assistente oficial para automações HTTP no CronFlow.

REGRAS:
1. ESCOPO & PYTHON/ML: O CronFlow agenda e dispara exclusivamente requisições HTTP/HTTPS (webhooks). NUNCA executa comandos locais, scripts Python (.py), notebooks ou processos locais. Scripts, pipelines ETL e modelos de ML devem ser expostos pelo usuário como API HTTP (FastAPI, Flask, Lambda, Cloud Function).
2. SEGURANÇA & SSRF: Recuse qualquer requisição para localhost, 127.0.0.1, IPs privados (RFC1918), link-local (169.254.x.x) ou metadados de nuvem.
3. FLUXO DE CRIAÇÃO E DISPARO:
   - Criar job: chame 'createJob' sem confirmationToken para obter o código 'CF-XXXXXX', apresente o resumo e peça confirmação.
   - Disparar job: chame 'listJobs' para obter o ID (UUID) da tarefa, em seguida chame 'triggerJob' com o jobId sem token para obter o código 'CF-XXXXXX' e peça confirmação.
   - Quando o usuário enviar o código 'CF-XXXXXX' ou confirmar ('confirmo', 'sim', 'ok'), execute IMEDIATAMENTE a ferramenta ('createJob' ou 'triggerJob') com o confirmationToken.
4. VERACIDADE: Baseie-se apenas nas ferramentas. Não invente tarefas, IDs ou rotas.`

// Chat — POST /v1/agent/chat
// @Summary Conversar com o Agente de IA do CronFlow
// @Description Envia uma mensagem para o Agente de IA, permitindo agendar jobs, testar cURLs, consultar histórico ou gerenciar tarefas por linguagem natural.
// @Tags AI Agent
// @Accept json
// @Produce json
// @Param body body AgentChatRequest true "Histórico e nova mensagem do usuário"
// @Success 200 {object} AgentChatResponse "Resposta do Agente de IA e histórico atualizado"
// @Failure 400 {object} map[string]string "Payload inválido ou sem mensagem"
// @Failure 401 {object} map[string]string "Não autenticado"
// @Failure 500 {object} map[string]string "Erros na API do Gemini/Groq ou durante a execução de ferramentas"
// @Security ApiKeyAuth
// @Router /v1/agent/chat [post]
func (h *AgentHandler) Chat(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Autorização inválida")
		return
	}

	limits, err := h.jobService.EntitlementEngine.GetUserLimits(r.Context(), proj.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao obter limites do plano")
		return
	}

	u, _ := h.userRepo.FindUserByProjectID(r.Context(), proj.ID)
	isProUser := u != nil && string(u.Plan) == "pro"

	var freeAiUsed int
	if !isProUser && !limits.WorkflowsEnabled {
		if u != nil {
			freeAiUsed = u.AiQueriesUsed
		} else if h.redis != nil {
			redisKey := fmt.Sprintf("ai_usage:%s", proj.UserID)
			usedVal, _ := h.redis.Get(r.Context(), redisKey).Int()
			freeAiUsed = usedVal
		}

		if freeAiUsed >= 3 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPaymentRequired)
			json.NewEncoder(w).Encode(map[string]any{
				"error": "Você atingiu o limite de 3 mensagens gratuitas da IA no Plano Free. Faça o upgrade para o Plano PRO para mensagens ilimitadas! 🚀",
				"code":  "FREE_AI_LIMIT_REACHED",
				"used":  freeAiUsed,
				"limit": 3,
			})
			return
		}

		newCount, err := h.userRepo.IncrementAIQueriesUsed(r.Context(), proj.UserID)
		if err == nil {
			freeAiUsed = newCount
		} else {
			freeAiUsed++
		}

		if h.redis != nil {
			redisKey := fmt.Sprintf("ai_usage:%s", proj.UserID)
			h.redis.Set(r.Context(), redisKey, freeAiUsed, 0)
		}
	}

	var input AgentChatRequest

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Body invalido")
		return
	}

	if h.cfg.GeminiAPIKey == "" && h.cfg.GroqAPIKey == "" {
		writeError(w, http.StatusInternalServerError, "Chaves de api do gemini/groq nao configuradas no backend")
		return
	}

	if len(input.Message) > 8*1024 {
		writeError(w, http.StatusBadRequest, "Mensagem muito longa (máximo 8KB)")
		return
	}

	if len(input.History) > 20 {
		input.History = input.History[len(input.History)-20:]
	}

	for _, msg := range input.History {
		if msg.Role != "user" && msg.Role != "model" && msg.Role != "function" {
			writeError(w, http.StatusBadRequest, "Histórico com papel de mensagem inválido")
			return
		}
		for _, part := range msg.Parts {
			if len(part.Text) > 16*1024 {
				writeError(w, http.StatusBadRequest, "Conteúdo do histórico excede o limite de tamanho permitido")
				return
			}
		}
	}

	// Prepare the conversation history
	history := input.History
	
	// Add user's new message to the history
	history = append(history, GeminiMessage{
		Role: "user",
		Parts: []GeminiPart{
			{Text: input.Message},
		},
	})

	ctx := r.Context()
	ctx = context.WithValue(ctx, userIDKey, proj.UserID)
	ctx = context.WithValue(ctx, clientIPKey, getClientIP(r))
	useGroq := h.cfg.GeminiAPIKey == "" || h.cfg.DisableGemini

	if useGroq {
		log.Println("🤖 Iniciando chat com o fallback Groq (LLaMA)")
		reply, updatedHistory, err := h.runGroqChat(ctx, proj.ID, history)
		if err != nil {
			log.Printf("ERROR Groq Chat Execution: %v", err)
			writeJSON(w, http.StatusOK, map[string]any{
				"reply":   "Não consegui processar essa solicitação agora. Não executei nenhuma ação. Tente novamente ou consulte o painel.",
				"history": history,
				"error": map[string]any{
					"code":      "AI_PROVIDER_UNAVAILABLE",
					"retryable": true,
				},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"reply":         reply,
			"history":       updatedHistory,
			"aiQueriesUsed": freeAiUsed,
		})
		return
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	geminiUrl := "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.5-flash-lite:generateContent?key=" + h.cfg.GeminiAPIKey

	// Execute up to 5 loop iterations to resolve function calls
	for loop := 0; loop < 5; loop++ {
		geminiReq := GeminiRequest{
			Contents: history,
			SystemInstruction: &GeminiSystemInstruction{
				Parts: []GeminiPart{{Text: systemInstruction}},
			},
			Tools: geminiTools,
			GenerationConfig: &GeminiGenerationConfig{
				Temperature:     0.1,
				TopP:            0.8,
				MaxOutputTokens: 900,
			},
		}

		reqBytes, err := json.Marshal(geminiReq)
		if err != nil {
			log.Printf("ERROR Gemini Marshal: %v", err)
			writeError(w, http.StatusInternalServerError, "Erro ao processar requisicao do agente")
			return
		}

		req, err := http.NewRequestWithContext(ctx, "POST", geminiUrl, bytes.NewReader(reqBytes))
		if err != nil {
			log.Printf("ERROR Gemini Request Creation: %v", err)
			writeError(w, http.StatusInternalServerError, "Erro ao criar chamada para IA")
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if err != nil {
				log.Printf("ERROR Gemini API Call: %v. Checking Groq fallback...", err)
			} else {
				var errBody map[string]any
				_ = json.NewDecoder(resp.Body).Decode(&errBody)
				log.Printf("ERROR Gemini API Status %d: %v. Checking Groq fallback...", resp.StatusCode, errBody)
				resp.Body.Close()
			}

			// Fallback to Groq if key exists
			if h.cfg.GroqAPIKey != "" {
				log.Println("🤖 Gemini indisponivel. Ativando fallback Groq (LLaMA)")
				reply, updatedHistory, err := h.runGroqChat(ctx, proj.ID, history)
				if err != nil {
					log.Printf("ERROR Groq Fallback Execution: %v", err)
					writeJSON(w, http.StatusOK, map[string]any{
						"reply":   "Não consegui processar essa solicitação agora. Não executei nenhuma ação. Tente novamente ou consulte o painel.",
						"history": history,
						"error": map[string]any{
							"code":      "AI_PROVIDER_UNAVAILABLE",
							"retryable": true,
						},
					})
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"reply":   reply,
					"history": updatedHistory,
				})
				return
			}

			writeJSON(w, http.StatusOK, map[string]any{
				"reply":   "Não consegui processar essa solicitação agora. Não executei nenhuma ação. Tente novamente ou consulte o painel.",
				"history": history,
				"error": map[string]any{
					"code":      "AI_PROVIDER_UNAVAILABLE",
					"retryable": true,
				},
			})
			return
		}
		defer resp.Body.Close()

		var geminiResp GeminiResponse
		if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
			log.Printf("ERROR Gemini Response Decode: %v", err)
			writeJSON(w, http.StatusOK, map[string]any{
				"reply":   "Não consegui processar essa solicitação agora. Não executei nenhuma ação. Tente novamente ou consulte o painel.",
				"history": history,
				"error": map[string]any{
					"code":      "AI_PROVIDER_UNAVAILABLE",
					"retryable": true,
				},
			})
			return
		}

		if len(geminiResp.Candidates) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{
				"reply":   "Não consegui processar essa solicitação agora. Não executei nenhuma ação. Tente novamente ou consulte o painel.",
				"history": history,
				"error": map[string]any{
					"code":      "AI_PROVIDER_UNAVAILABLE",
					"retryable": true,
				},
			})
			return
		}

		candidate := geminiResp.Candidates[0]
		
		// Append model's response to history
		history = append(history, candidate.Content)

		hasFunctionCall := false
		var toolParts []GeminiPart

		for _, part := range candidate.Content.Parts {
			if part.FunctionCall != nil {
				hasFunctionCall = true
				result, err := h.executeTool(ctx, proj.ID, part.FunctionCall.Name, part.FunctionCall.Args)
				var responseMap map[string]any
				if err != nil {
					responseMap = map[string]any{"error": err.Error()}
				} else {
					responseMap = map[string]any{"result": result}
				}

				toolParts = append(toolParts, GeminiPart{
					FunctionResponse: &GeminiFunctionResponse{
						ID:       part.FunctionCall.ID,
						Name:     part.FunctionCall.Name,
						Response: responseMap,
					},
				})
			}
		}

		if !hasFunctionCall {
			var reply string
			for _, part := range candidate.Content.Parts {
				if part.Text != "" {
					reply = part.Text
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"reply":         reply,
				"history":       history,
				"aiQueriesUsed": freeAiUsed,
			})
			return
		}

		history = append(history, GeminiMessage{
			Role:  "user",
			Parts: toolParts,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"reply":   "Excedeu o limite de chamadas de função do agente para esta solicitação. Nenhuma ação foi finalizada.",
		"history": history,
		"error": map[string]any{
			"code":      "TOOL_CALL_LIMIT_EXCEEDED",
			"retryable": false,
		},
	})
}

func (h *AgentHandler) executeTool(ctx context.Context, projectID string, name string, args map[string]any) (any, error) {
	switch name {
	case "listJobs":
		jobs, err := h.jobService.List(ctx, projectID)
		if err != nil {
			return nil, err
		}
		type JobSummary struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Schedule string `json:"schedule"`
			URL      string `json:"url"`
			Method   string `json:"httpMethod"`
			Status   string `json:"status"`
		}
		var summaries []JobSummary
		for _, j := range jobs {
			summaries = append(summaries, JobSummary{
				ID:       j.ID,
				Name:     j.Name,
				Schedule: j.Schedule,
				URL:      j.URL,
				Method:   string(j.HTTPMethod),
				Status:   string(j.Status),
			})
		}
		return summaries, nil

	case "createJob":
		nameVal, _ := args["name"].(string)
		scheduleVal, _ := args["schedule"].(string)
		urlVal, _ := args["url"].(string)
		if nameVal == "" || scheduleVal == "" || urlVal == "" {
			return nil, fmt.Errorf("name, schedule and url are required")
		}

		httpMethodVal, _ := args["httpMethod"].(string)
		if httpMethodVal == "" {
			httpMethodVal = "POST"
		}
		methodUpper := strings.ToUpper(httpMethodVal)
		if methodUpper != "GET" && methodUpper != "POST" && methodUpper != "PUT" && methodUpper != "DELETE" && methodUpper != "PATCH" {
			return nil, fmt.Errorf("método HTTP inválido: %s. Use GET, POST, PUT, DELETE ou PATCH", httpMethodVal)
		}

		headersVal := make(map[string]string)
		if headersStr, ok := args["headers"].(string); ok && headersStr != "" {
			if err := json.Unmarshal([]byte(headersStr), &headersVal); err != nil {
				return nil, fmt.Errorf("headers deve ser um JSON válido: %w", err)
			}
		}

		var payloadVal map[string]any
		if payloadStr, ok := args["payload"].(string); ok && payloadStr != "" {
			if err := json.Unmarshal([]byte(payloadStr), &payloadVal); err != nil {
				return nil, fmt.Errorf("payload deve ser um objeto JSON válido: %w", err)
			}
		}

		var webhookAlertUrl *string
		if alertUrl, ok := args["webhookAlertUrl"].(string); ok && alertUrl != "" {
			webhookAlertUrl = &alertUrl
		}

		if err := httputil.ValidateURL(ctx, urlVal); err != nil {
			return nil, fmt.Errorf("URL inválida: %w", err)
		}

		if webhookAlertUrl != nil && *webhookAlertUrl != "" {
			if err := httputil.ValidateURL(ctx, *webhookAlertUrl); err != nil {
				return nil, fmt.Errorf("webhookAlertUrl inválida: %w", err)
			}
		}

		normArgs, err := normalizeCreateJobArgs(args)
		if err != nil {
			return nil, fmt.Errorf("erro ao normalizar argumentos: %w", err)
		}
		argsHash := computeHash(normArgs)

		token, _ := args["confirmationToken"].(string)
		if token == "" {
			newToken := generateToken()
			if err := h.setPendingOp(ctx, newToken, PendingOperation{
				Tool:      "createJob",
				ArgsHash:  argsHash,
				ProjectID: projectID,
				ExpiresAt: time.Now().Add(5 * time.Minute),
			}); err != nil {
				return nil, err
			}
			return map[string]any{
				"status":            "CONFIRMATION_REQUIRED",
				"confirmationToken": newToken,
				"message":           "Ação pendente. Por favor, apresente um resumo detalhado dos parâmetros (Nome, URL, Schedule, Método) e solicite que o usuário digite exatamente o código para confirmar: " + newToken,
			}, nil
		}

		if err := h.consumePendingOp(ctx, token, "createJob", projectID, argsHash); err != nil {
			return nil, err
		}

		created, err := h.jobService.Create(ctx, service.CreateJobInput{
			ProjectID:       projectID,
			Name:            nameVal,
			Schedule:        scheduleVal,
			URL:             urlVal,
			HTTPMethod:      job.HTTPMethod(methodUpper),
			Headers:         headersVal,
			Payload:         payloadVal,
			WebhookAlertURL: webhookAlertUrl,
		})
		if err != nil {
			return nil, err
		}
		return created, nil

	case "triggerJob":
		jobID, _ := args["jobId"].(string)
		if jobID == "" {
			return nil, fmt.Errorf("jobId is required")
		}

		normArgs, err := normalizeTriggerJobArgs(args)
		if err != nil {
			return nil, fmt.Errorf("erro ao normalizar argumentos: %w", err)
		}
		argsHash := computeHash(normArgs)

		token, _ := args["confirmationToken"].(string)
		if token == "" {
			newToken := generateToken()
			if err := h.setPendingOp(ctx, newToken, PendingOperation{
				Tool:      "triggerJob",
				ArgsHash:  argsHash,
				ProjectID: projectID,
				ExpiresAt: time.Now().Add(5 * time.Minute),
			}); err != nil {
				return nil, err
			}
			return map[string]any{
				"status":            "CONFIRMATION_REQUIRED",
				"confirmationToken": newToken,
				"message":           "Ação pendente. Por favor, solicite que o usuário confirme o disparo da tarefa digitando exatamente o código: " + newToken,
			}, nil
		}

		if err := h.consumePendingOp(ctx, token, "triggerJob", projectID, argsHash); err != nil {
			return nil, err
		}

		err = h.jobService.TriggerNow(ctx, jobID, projectID)
		if err != nil {
			return nil, err
		}
		return map[string]string{"status": "success", "message": "tarefa enfileirada para execucao imediata"}, nil

	case "executeCurlDirect":
		urlVal, _ := args["url"].(string)
		if urlVal == "" {
			return nil, fmt.Errorf("url is required")
		}

		methodVal, _ := args["method"].(string)
		if methodVal == "" {
			methodVal = "GET"
		}

		headersVal := make(map[string]string)
		if headersStr, ok := args["headers"].(string); ok && headersStr != "" {
			if err := json.Unmarshal([]byte(headersStr), &headersVal); err != nil {
				return nil, fmt.Errorf("headers deve ser um JSON válido: %w", err)
			}
		}

		var payloadBytes []byte
		if payloadStr, ok := args["payload"].(string); ok && payloadStr != "" {
			payloadBytes = []byte(payloadStr)
		}

		// Executa a requisição
		reqClient := httputil.SafeClient()
		req, err := http.NewRequest(methodVal, urlVal, bytes.NewReader(payloadBytes))
		if err != nil {
			return nil, fmt.Errorf("falha ao criar requisicao: %w", err)
		}

		for k, v := range headersVal {
			req.Header.Set(k, v)
		}
		if len(payloadBytes) > 0 && req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/json")
		}

		startTime := time.Now()
		resp, err := reqClient.Do(req)
		duration := time.Since(startTime)

		if err != nil {
			return map[string]any{
				"status":  "Error",
				"reason":  err.Error(),
				"latency": fmt.Sprintf("%dms", duration.Milliseconds()),
			}, nil
		}
		defer resp.Body.Close()

		bodyBytes, _ := io.ReadAll(resp.Body)
		bodyStr := string(bodyBytes)
		if len(bodyStr) > 1000 {
			bodyStr = bodyStr[:1000] + "\n...(truncado)"
		}

		respHeaders := make(map[string]string)
		for k, v := range resp.Header {
			if len(v) > 0 {
				respHeaders[k] = v[0]
			}
		}

		return map[string]any{
			"status":  resp.Status,
			"code":    resp.StatusCode,
			"latency": fmt.Sprintf("%dms", duration.Milliseconds()),
			"headers": respHeaders,
			"body":    bodyStr,
		}, nil

	default:
		return nil, fmt.Errorf("tool desconhecida: %s", name)
	}
}

// Groq Fallback logic implementation
func (h *AgentHandler) runGroqChat(ctx context.Context, projectID string, history []GeminiMessage) (string, []GeminiMessage, error) {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	groqUrl := "https://api.groq.com/openai/v1/chat/completions"

	openaiTools := getOpenAITools()
	openaiHistory := translateGeminiToOpenAI(history)
	if len(openaiHistory) > 6 {
		openaiHistory = openaiHistory[len(openaiHistory)-6:]
	}

	modelsToTry := []string{"openai/gpt-oss-20b", "openai/gpt-oss-120b", "qwen/qwen3.8-27b"}
	selectedModel := "openai/gpt-oss-20b"

	for loop := 0; loop < 5; loop++ {
		messages := []OpenAIMessage{
			{
				Role:    "system",
				Content: systemInstruction,
			},
		}
		messages = append(messages, openaiHistory...)

		var groqResp OpenAIResponse
		var lastErr error

		// Try models in fallback order if rate limited
		for _, modelName := range modelsToTry {
			groqReq := OpenAIRequest{
				Model:       modelName,
				Messages:    messages,
				Tools:       openaiTools,
				Temperature: 0.1,
				TopP:        0.8,
				MaxTokens:   1200,
			}

			reqBytes, err := json.Marshal(groqReq)
			if err != nil {
				return "", nil, err
			}

			req, err := http.NewRequestWithContext(ctx, "POST", groqUrl, bytes.NewReader(reqBytes))
			if err != nil {
				return "", nil, err
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+h.cfg.GroqAPIKey)

			resp, err := httpClient.Do(req)
			if err != nil {
				lastErr = err
				continue
			}

			if resp.StatusCode == http.StatusOK {
				selectedModel = modelName
				err := json.NewDecoder(resp.Body).Decode(&groqResp)
				resp.Body.Close()
				if err == nil && len(groqResp.Choices) > 0 {
					lastErr = nil
					break
				}
			} else {
				var errBody map[string]any
				_ = json.NewDecoder(resp.Body).Decode(&errBody)
				resp.Body.Close()
				log.Printf("Groq model %s returned status %d: %v. Trying next model...", modelName, resp.StatusCode, errBody)
				lastErr = fmt.Errorf("Groq HTTP %d: %v", resp.StatusCode, errBody)
			}
		}

		if lastErr != nil || len(groqResp.Choices) == 0 {
			if lastErr == nil {
				lastErr = fmt.Errorf("sem choices do Groq")
			}
			return "", nil, lastErr
		}

		_ = selectedModel
		choice := groqResp.Choices[0]
		assistantMsg := choice.Message

		openaiHistory = append(openaiHistory, assistantMsg)

		if len(assistantMsg.ToolCalls) == 0 {
			var finalHistory []GeminiMessage
			for _, m := range openaiHistory {
				finalHistory = append(finalHistory, translateOpenAIToGemini(m))
			}
			return assistantMsg.Content, finalHistory, nil
		}

		// Executa ferramentas para o Groq
		for _, tc := range assistantMsg.ToolCalls {
			var args map[string]any
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)

			result, err := h.executeTool(ctx, projectID, tc.Function.Name, args)
			var responseMap map[string]any
			if err != nil {
				responseMap = map[string]any{"error": err.Error()}
			} else {
				responseMap = map[string]any{"result": result}
			}

			respStr, _ := json.Marshal(responseMap)

			openaiHistory = append(openaiHistory, OpenAIMessage{
				Role:       "tool",
				Content:    string(respStr),
				ToolCallID: tc.ID,
			})
		}
	}

	return "", nil, fmt.Errorf("excedeu loop de tools no Groq fallback")
}

func toOpenAISchema(gs GeminiSchema) OpenAISchema {
	props := make(map[string]OpenAISchema)
	for k, v := range gs.Properties {
		props[k] = toOpenAISchema(v)
	}
	return OpenAISchema{
		Type:        strings.ToLower(gs.Type),
		Properties:  props,
		Required:    gs.Required,
		Description: gs.Description,
	}
}

func getOpenAITools() []OpenAITool {
	var oTools []OpenAITool
	for _, gt := range geminiTools {
		for _, fd := range gt.FunctionDeclarations {
			oTools = append(oTools, OpenAITool{
				Type: "function",
				Function: OpenAIFunctionDecl{
					Name:        fd.Name,
					Description: fd.Description,
					Parameters:  toOpenAISchema(fd.Parameters),
				},
			})
		}
	}
	return oTools
}

func translateGeminiToOpenAI(history []GeminiMessage) []OpenAIMessage {
	var openaiMsgs []OpenAIMessage
	for _, msg := range history {
		role := msg.Role
		if role == "model" {
			role = "assistant"
		}

		var content string
		var toolCalls []OpenAIToolCall
		var isToolTurn bool
		var toolCallID string

		for _, part := range msg.Parts {
			if part.Text != "" {
				content = part.Text
			} else if part.FunctionCall != nil {
				role = "assistant"
				toolCalls = append(toolCalls, OpenAIToolCall{
					ID:   part.FunctionCall.ID,
					Type: "function",
					Function: OpenAIFunction{
						Name:      part.FunctionCall.Name,
						Arguments: marshalArgs(part.FunctionCall.Args),
					},
				})
			} else if part.FunctionResponse != nil {
				isToolTurn = true
				role = "tool"
				toolCallID = part.FunctionResponse.ID
				content = marshalArgs(part.FunctionResponse.Response)
			}
		}

		if isToolTurn {
			openaiMsgs = append(openaiMsgs, OpenAIMessage{
				Role:       "tool",
				Content:    content,
				ToolCallID: toolCallID,
			})
		} else {
			openaiMsgs = append(openaiMsgs, OpenAIMessage{
				Role:      role,
				Content:   content,
				ToolCalls: toolCalls,
			})
		}
	}
	return openaiMsgs
}

func translateOpenAIToGemini(openaiMsg OpenAIMessage) GeminiMessage {
	role := openaiMsg.Role
	if role == "assistant" {
		role = "model"
	} else if role == "tool" {
		role = "user"
	}

	var parts []GeminiPart
	if openaiMsg.Role == "tool" || openaiMsg.ToolCallID != "" {
		var resp map[string]any
		_ = json.Unmarshal([]byte(openaiMsg.Content), &resp)
		parts = append(parts, GeminiPart{
			FunctionResponse: &GeminiFunctionResponse{
				ID:       openaiMsg.ToolCallID,
				Name:     "",
				Response: resp,
			},
		})
	} else {
		if openaiMsg.Content != "" {
			parts = append(parts, GeminiPart{Text: openaiMsg.Content})
		}

		for _, tc := range openaiMsg.ToolCalls {
			var args map[string]any
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			parts = append(parts, GeminiPart{
				FunctionCall: &GeminiFunctionCall{
					ID:   tc.ID,
					Name: tc.Function.Name,
					Args: args,
				},
			})
		}
	}

	return GeminiMessage{
		Role:  role,
		Parts: parts,
	}
}

func marshalArgs(args map[string]any) string {
	b, _ := json.Marshal(args)
	return string(b)
}



type NormalizedCreateJob struct {
	Name            string            `json:"name"`
	Schedule        string            `json:"schedule"`
	URL             string            `json:"url"`
	HTTPMethod      string            `json:"httpMethod"`
	Headers         map[string]string `json:"headers,omitempty"`
	Payload         map[string]any    `json:"payload,omitempty"`
	WebhookAlertURL string            `json:"webhookAlertUrl,omitempty"`
}

type NormalizedTriggerJob struct {
	JobID string `json:"jobId"`
}

func normalizeCreateJobArgs(args map[string]any) (NormalizedCreateJob, error) {
	name, _ := args["name"].(string)
	schedule, _ := args["schedule"].(string)
	url, _ := args["url"].(string)
	
	httpMethod, _ := args["httpMethod"].(string)
	if httpMethod == "" {
		httpMethod = "POST"
	}
	httpMethod = strings.ToUpper(httpMethod)

	headers := make(map[string]string)
	if hStr, ok := args["headers"].(string); ok && hStr != "" {
		if err := json.Unmarshal([]byte(hStr), &headers); err != nil {
			return NormalizedCreateJob{}, fmt.Errorf("headers inválido: %w", err)
		}
	}

	var payload map[string]any
	if pStr, ok := args["payload"].(string); ok && pStr != "" {
		if err := json.Unmarshal([]byte(pStr), &payload); err != nil {
			return NormalizedCreateJob{}, fmt.Errorf("payload inválido: %w", err)
		}
	}

	webhookAlertUrl, _ := args["webhookAlertUrl"].(string)

	return NormalizedCreateJob{
		Name:            name,
		Schedule:        schedule,
		URL:             url,
		HTTPMethod:      httpMethod,
		Headers:         headers,
		Payload:         payload,
		WebhookAlertURL: webhookAlertUrl,
	}, nil
}

func normalizeTriggerJobArgs(args map[string]any) (NormalizedTriggerJob, error) {
	jobID, _ := args["jobId"].(string)
	return NormalizedTriggerJob{
		JobID: jobID,
	}, nil
}

func computeHash(val any) string {
	b, _ := json.Marshal(val)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (h *AgentHandler) setPendingOp(ctx context.Context, token string, op PendingOperation) error {
	if h.redis == nil {
		if h.cfg.AppEnv == "production" || os.Getenv("APP_ENV") == "production" {
			return fmt.Errorf("AI_CONFIRMATION_STORE_UNAVAILABLE: Armazenamento de confirmações indisponível")
		}
		h.pendingMu.Lock()
		defer h.pendingMu.Unlock()
		h.pendingOps[token] = op
		return nil
	}
	key := "cronflow:agent:pending:" + token
	data, err := json.Marshal(op)
	if err != nil {
		return err
	}
	return h.redis.Set(ctx, key, data, 5*time.Minute).Err()
}

func (h *AgentHandler) incrementFailures(ctx context.Context, projectID string) {
	if h.redis == nil {
		return
	}
	userID, _ := ctx.Value(userIDKey).(string)
	clientIP, _ := ctx.Value(clientIPKey).(string)
	key := fmt.Sprintf("cronflow:agent:limit:%s:%s:%s", projectID, userID, clientIP)

	const luaRateLimit = `
		local val = redis.call("INCR", KEYS[1])
		if val == 1 then
			redis.call("EXPIRE", KEYS[1], 60)
		end
		return val
	`
	_, _ = h.redis.Eval(ctx, luaRateLimit, []string{key}).Result()
}

func (h *AgentHandler) checkFailures(ctx context.Context, projectID string) error {
	if h.redis == nil {
		return nil
	}
	userID, _ := ctx.Value(userIDKey).(string)
	clientIP, _ := ctx.Value(clientIPKey).(string)
	key := fmt.Sprintf("cronflow:agent:limit:%s:%s:%s", projectID, userID, clientIP)

	val, err := h.redis.Get(ctx, key).Int()
	if err == nil && val >= 5 {
		return fmt.Errorf("limite de tentativas de confirmação excedido para o seu usuário. Aguarde 1 minuto")
	}
	return nil
}

func (h *AgentHandler) consumePendingOp(ctx context.Context, token string, expectedTool string, projectID string, currentArgsHash string) error {
	if err := h.checkFailures(ctx, projectID); err != nil {
		return err
	}

	var op PendingOperation
	var found bool

	if h.redis == nil {
		if h.cfg.AppEnv == "production" || os.Getenv("APP_ENV") == "production" {
			return fmt.Errorf("AI_CONFIRMATION_STORE_UNAVAILABLE: Armazenamento de confirmações indisponível")
		}
		h.pendingMu.Lock()
		op, found = h.pendingOps[token]
		if !found {
			h.pendingMu.Unlock()
			return fmt.Errorf("código de confirmação inválido, expirado ou já utilizado")
		}

		if op.Tool != expectedTool || op.ProjectID != projectID || subtle.ConstantTimeCompare([]byte(op.ArgsHash), []byte(currentArgsHash)) != 1 {
			h.pendingMu.Unlock()
			return fmt.Errorf("os parâmetros da requisição foram alterados desde a confirmação (tampering detectado). Por favor, repita o processo")
		}

		delete(h.pendingOps, token)
		h.pendingMu.Unlock()
	} else {
		key := "cronflow:agent:pending:" + token
		const luaConsume = `
			local val = redis.call("GET", KEYS[1])
			if not val then
				return "NOT_FOUND"
			end
			local ok, op = pcall(cjson.decode, val)
			if not ok or not op then
				return "MALFORMED"
			end
			if op.Tool ~= ARGV[1] or op.ProjectID ~= ARGV[2] or op.ArgsHash ~= ARGV[3] then
				return "TAMPERING"
			end
			redis.call("DEL", KEYS[1])
			return val
		`
		res, err := h.redis.Eval(ctx, luaConsume, []string{key}, expectedTool, projectID, currentArgsHash).Result()
		if err != nil {
			h.incrementFailures(ctx, projectID)
			return fmt.Errorf("código de confirmação inválido, expirado ou já utilizado")
		}

		valStr, ok := res.(string)
		if !ok || valStr == "" {
			h.incrementFailures(ctx, projectID)
			return fmt.Errorf("código de confirmação inválido, expirado ou já utilizado")
		}

		if valStr == "NOT_FOUND" {
			h.incrementFailures(ctx, projectID)
			return fmt.Errorf("código de confirmação inválido, expirado ou já utilizado")
		}

		if valStr == "TAMPERING" {
			return fmt.Errorf("os parâmetros da requisição foram alterados desde a confirmação (tampering detectado). Por favor, repita o processo")
		}

		if valStr == "MALFORMED" {
			return fmt.Errorf("erro ao processar dados da confirmação pendente")
		}

		if err := json.Unmarshal([]byte(valStr), &op); err != nil {
			return fmt.Errorf("erro ao decodificar dados da confirmação: %w", err)
		}
	}

	return nil
}

func generateToken() string {
	const letters = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return "CF-" + string(b)
}


