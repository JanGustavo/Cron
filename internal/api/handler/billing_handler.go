package handler

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/JanGustavo/Cron/internal/api/middleware"
	"github.com/JanGustavo/Cron/internal/config"
	"github.com/JanGustavo/Cron/internal/domain/billing"
	"github.com/JanGustavo/Cron/internal/service"
)

type BillingHandler struct {
	entitlementEngine *service.EntitlementEngine
	cfg               *config.Config
	asaasSync         *service.AsaasReconciler
}

func NewBillingHandler(entitlementEngine *service.EntitlementEngine, cfg *config.Config) *BillingHandler {
	return &BillingHandler{
		entitlementEngine: entitlementEngine,
		cfg:               cfg,
		asaasSync:         service.NewAsaasReconciler(entitlementEngine.AsaasStorage(), service.NewAsaasClient(cfg.AsaasAPIKey, cfg.AppEnv == "production" || strings.HasPrefix(cfg.AsaasAPIKey, "$aact_prod_"))),
	}
}

type checkoutReq struct {
	SuccessURL string `json:"success_url"`
	CancelURL  string `json:"cancel_url"`
	Period     string `json:"period"` // "monthly" (padrão) ou "yearly"
}

// CreateCheckoutSession inicia a sessão de checkout segura da Stripe
// POST /v1/billing/checkout
func (h *BillingHandler) CreateCheckoutSession(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Não autorizado")
		return
	}

	var req checkoutReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Corpo da requisição inválido")
		return
	}

	if h.cfg.BillingProvider == "asaas" {
		value := 29.00
		cycle := "MONTHLY"
		if req.Period == "yearly" {
			value = 290.00
			cycle = "YEARLY"
		}

		bodyMap := map[string]interface{}{
			"name":              "CronFlow PRO",
			"description":       "Assinatura do Plano PRO no CronFlow",
			"value":             value,
			"billingType":       "UNDEFINED",
			"chargeType":        "RECURRENT",
			"subscriptionCycle": cycle,
			"dueDateLimitDays":  5,
			"externalReference": proj.UserID,
		}

		respBody, statusCode, err := h.callAsaas("POST", "/paymentLinks", bodyMap)
		if err != nil {
			log.Printf("Erro ao chamar Asaas API: %v (status: %d)", err, statusCode)
			writeError(w, http.StatusInternalServerError, "Erro ao criar link de pagamento no Asaas")
			return
		}

		if statusCode >= 400 {
			log.Printf("Asaas API retornou erro: %s (status: %d)", string(respBody), statusCode)
			writeError(w, http.StatusInternalServerError, "Erro na integração com gateway de faturamento")
			return
		}

		var asaasResp struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		}
		if err := json.Unmarshal(respBody, &asaasResp); err != nil {
			writeError(w, http.StatusInternalServerError, "Erro ao decodificar resposta do gateway de faturamento")
			return
		}

		if asaasResp.ID == "" || asaasResp.URL == "" {
			writeError(w, http.StatusBadGateway, "Resposta de checkout inválida")
			return
		}
		if err := h.asaasSync.RecordCheckout(r.Context(), proj.UserID, asaasResp.ID, cycle); err != nil {
			writeError(w, http.StatusInternalServerError, "Não foi possível registrar o checkout")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"url": asaasResp.URL,
		})
		return
	}

	writeError(w, http.StatusBadRequest, "O gateway Stripe foi desativado. Por favor, utilize o Asaas configurando BILLING_PROVIDER=asaas.")
}

// CreatePortalSession gera a URL de redirecionamento para o Customer Portal da Stripe
// POST /v1/billing/portal
func (h *BillingHandler) CreatePortalSession(w http.ResponseWriter, r *http.Request) {
	proj := middleware.ProjectFromContext(r.Context())
	if proj == nil {
		writeError(w, http.StatusUnauthorized, "Não autorizado")
		return
	}

	sub, err := h.entitlementEngine.GetSubscription(r.Context(), proj.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao consultar assinatura do usuário")
		return
	}

	if h.cfg.BillingProvider == "asaas" {
		if sub == nil || sub.ProviderSubscriptionID == nil || *sub.ProviderSubscriptionID == "" {
			writeError(w, http.StatusBadRequest, "Você não possui uma assinatura ou cliente ativo no Asaas. Inicie a assinatura pelo botão Assinar PRO.")
			return
		}

		subID := *sub.ProviderSubscriptionID

		if strings.HasPrefix(subID, "lnk_") {
			var linkURL string
			respBody, statusCode, err := h.callAsaas("GET", "/paymentLinks/"+subID, nil)
			if err == nil && statusCode < 400 {
				var linkResp struct {
					URL string `json:"url"`
				}
				if json.Unmarshal(respBody, &linkResp) == nil && linkResp.URL != "" {
					linkURL = linkResp.URL
				}
			}
			if linkURL == "" {
				linkURL = h.cfg.FrontendURL + "/profile"
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"url": linkURL,
			})
			return
		}

		var paymentsResp struct {
			Data []struct {
				InvoiceURL string `json:"invoiceUrl"`
			} `json:"data"`
		}
		respBody, statusCode, err := h.callAsaas("GET", "/payments?subscription="+subID+"&limit=1", nil)
		var redirectURL string
		if err == nil && statusCode < 400 {
			if json.Unmarshal(respBody, &paymentsResp) == nil && len(paymentsResp.Data) > 0 {
				redirectURL = paymentsResp.Data[0].InvoiceURL
			}
		}
		if redirectURL == "" {
			redirectURL = h.cfg.FrontendURL + "/profile"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"url": redirectURL,
		})
		return
	}

	writeError(w, http.StatusBadRequest, "O gateway Stripe foi desativado. Por favor, utilize o Asaas configurando BILLING_PROVIDER=asaas.")
}

// Webhook recebe, valida a assinatura e processa eventos emitidos pela Stripe ou Asaas
// POST /v1/billing/webhook
func (h *BillingHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	const MaxBodySize = 1048576 // 1MB
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodySize)
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Corpo da requisição muito longo ou inválido")
		return
	}

	// Verifica se é um Webhook do Asaas
	asaasToken := r.Header.Get("asaas-access-token")
	if asaasToken == "" {
		asaasToken = r.Header.Get("Asaas-Access-Token")
	}

	if asaasToken != "" {
		h.ProcessAsaasWebhook(w, r, payload, asaasToken)
		return
	}
	writeError(w, http.StatusBadRequest, "Assinatura do webhook Stripe desativada.")
}

func (h *BillingHandler) callAsaas(method, path string, body interface{}) ([]byte, int, error) {
	var asaasBaseURL string
	if strings.HasPrefix(h.cfg.AsaasAPIKey, "$aact_hmlg_") {
		asaasBaseURL = "https://api-sandbox.asaas.com/v3"
	} else if strings.HasPrefix(h.cfg.AsaasAPIKey, "$aact_prod_") || h.cfg.AppEnv == "production" {
		asaasBaseURL = "https://api.asaas.com/v3"
	} else {
		asaasBaseURL = "https://api-sandbox.asaas.com/v3"
	}

	url := asaasBaseURL + path
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reqBody = bytes.NewBuffer(b)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("access_token", h.cfg.AsaasAPIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return respBody, resp.StatusCode, nil
}

type AsaasWebhookPayload = billing.AsaasEvent
type AsaasPayment = billing.AsaasPayment
type AsaasSubscription = billing.AsaasSubscription

func (h *BillingHandler) SetAsaasReconciler(sync *service.AsaasReconciler) { h.asaasSync = sync }
func (h *BillingHandler) ProcessAsaasWebhook(w http.ResponseWriter, r *http.Request, payload []byte, token string) {
	if h.cfg.AsaasWebhookToken == "" || token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(h.cfg.AsaasWebhookToken)) != 1 {
		writeError(w, http.StatusUnauthorized, "Token de webhook inválido")
		return
	}
	var event billing.AsaasEvent
	if err := json.Unmarshal(payload, &event); err != nil || event.Event == "" {
		writeError(w, http.StatusBadRequest, "Payload inválido")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	status, err := h.asaasSync.ProcessEvent(ctx, payload)
	if err != nil {
		log.Printf("Asaas webhook: %v", err)
		if status != "pending_retry" {
			writeError(w, http.StatusServiceUnavailable, "Não foi possível persistir o evento")
			return
		}
		// Durable receipt is acknowledged even if provider verification must retry.
		// Successful financial processing is marked only in the state-update transaction.
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": status})
}
