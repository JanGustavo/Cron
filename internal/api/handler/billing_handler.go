package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
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
}

func NewBillingHandler(entitlementEngine *service.EntitlementEngine, cfg *config.Config) *BillingHandler {
	return &BillingHandler{
		entitlementEngine: entitlementEngine,
		cfg:               cfg,
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

		subRecord, err := h.entitlementEngine.GetSubscription(r.Context(), proj.UserID)
		if err != nil {
			log.Printf("Erro ao consultar assinatura local: %v", err)
		}
		nowTime := time.Now()
		if subRecord == nil {
			subRecord = &billing.Subscription{
				UserID:                 proj.UserID,
				PlanCode:               "pro",
				Status:                 "pending",
				BillingProvider:        "asaas",
				ProviderSubscriptionID: &asaasResp.ID,
				CreatedAt:              nowTime,
				UpdatedAt:              nowTime,
			}
		} else {
			subRecord.BillingProvider = "asaas"
			subRecord.Status = "pending"
			subRecord.ProviderSubscriptionID = &asaasResp.ID
			subRecord.PlanCode = "pro"
			subRecord.UpdatedAt = nowTime
		}
		if err := h.entitlementEngine.UpsertSubscription(r.Context(), subRecord); err != nil {
			log.Printf("Erro ao atualizar assinatura pendente no banco: %v", err)
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
		asaasBaseURL = "https://sandbox.asaas.com/api/v3"
	} else if h.cfg.AppEnv == "production" {
		asaasBaseURL = "https://api.asaas.com/v3"
	} else {
		asaasBaseURL = "https://sandbox.asaas.com/api/v3"
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

type AsaasWebhookPayload struct {
	Event        string            `json:"event"`
	Payment      *AsaasPayment      `json:"payment,omitempty"`
	Subscription *AsaasSubscription `json:"subscription,omitempty"`
}

type AsaasPayment struct {
	ID                string  `json:"id"`
	Customer          string  `json:"customer"`
	Subscription      string  `json:"subscription"`
	PaymentLink       string  `json:"paymentLink"`
	Status            string  `json:"status"`
	Value             float64 `json:"value"`
	BillingType       string  `json:"billingType"`
	ExternalReference string  `json:"externalReference"`
}

type AsaasSubscription struct {
	ID                string  `json:"id"`
	Customer          string  `json:"customer"`
	Status            string  `json:"status"`
	Value             float64 `json:"value"`
	BillingType       string  `json:"billingType"`
	ExternalReference string  `json:"externalReference"`
}

func (h *BillingHandler) ProcessAsaasWebhook(w http.ResponseWriter, r *http.Request, payload []byte, token string) {
	if token != h.cfg.AsaasWebhookToken {
		log.Printf("Token do webhook Asaas inválido: recebido=%s", token)
		writeError(w, http.StatusUnauthorized, "Token de autenticação do webhook inválido")
		return
	}

	var payloadData AsaasWebhookPayload
	if err := json.Unmarshal(payload, &payloadData); err != nil {
		log.Printf("Erro ao fazer parse do payload Asaas: %v", err)
		writeError(w, http.StatusBadRequest, "Payload inválido")
		return
	}

	eventID := ""
	if payloadData.Payment != nil {
		eventID = payloadData.Payment.ID + "_" + payloadData.Event
	} else if payloadData.Subscription != nil {
		eventID = payloadData.Subscription.ID + "_" + payloadData.Event
	} else {
		eventID = fmt.Sprintf("%d_%s", time.Now().UnixNano(), payloadData.Event)
	}

	billingEvent, isDuplicate, err := h.entitlementEngine.RegisterBillingEvent(
		r.Context(),
		"asaas",
		eventID,
		payloadData.Event,
		nil,
		payload,
	)
	if err != nil {
		log.Printf("Erro ao registrar evento Asaas: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro interno ao processar transação")
		return
	}

	if isDuplicate {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ignored_duplicate"}`))
		return
	}

	var processingError error

	switch payloadData.Event {
	case "PAYMENT_RECEIVED", "PAYMENT_CONFIRMED":
		if payloadData.Payment != nil {
			pay := payloadData.Payment
			var localSub *billing.Subscription

			if pay.PaymentLink != "" {
				localSub, err = h.entitlementEngine.GetSubscriptionByProviderSubID(r.Context(), pay.PaymentLink)
			}
			if (err != nil || localSub == nil) && pay.Subscription != "" {
				localSub, err = h.entitlementEngine.GetSubscriptionByProviderSubID(r.Context(), pay.Subscription)
			}
			if (err != nil || localSub == nil) && pay.ExternalReference != "" {
				localSub, err = h.entitlementEngine.GetSubscription(r.Context(), pay.ExternalReference)
			}

			if localSub != nil {
				now := time.Now()
				var nextPeriod time.Time

				// Se o valor for maior que R$ 150, é plano anual (+1 ano), senão mensal (+1 mês)
				if pay.Value > 150.0 {
					nextPeriod = now.AddDate(1, 0, 0)
				} else {
					nextPeriod = now.AddDate(0, 1, 0)
				}

				localSub.PlanCode = "pro"
				localSub.Status = "active"
				localSub.BillingProvider = "asaas"
				localSub.ProviderCustomerID = &pay.Customer
				if pay.Subscription != "" {
					localSub.ProviderSubscriptionID = &pay.Subscription
				} else if pay.PaymentLink != "" {
					localSub.ProviderSubscriptionID = &pay.PaymentLink
				}
				localSub.CurrentPeriodStart = &now
				localSub.CurrentPeriodEnd = &nextPeriod
				localSub.CancelAtPeriodEnd = false

				processingError = h.entitlementEngine.UpsertSubscription(r.Context(), localSub)
			} else {
				log.Printf("Aviso: Pagamento Asaas recebido mas nenhuma assinatura local correspondente foi encontrada para link=%s, sub=%s, user=%s", pay.PaymentLink, pay.Subscription, pay.ExternalReference)
			}
		}

	case "PAYMENT_REFUNDED", "PAYMENT_CHARGEBACK_REQUESTED":
		if payloadData.Payment != nil {
			pay := payloadData.Payment
			var localSub *billing.Subscription
			if pay.Subscription != "" {
				localSub, _ = h.entitlementEngine.GetSubscriptionByProviderSubID(r.Context(), pay.Subscription)
			}
			if localSub == nil && pay.PaymentLink != "" {
				localSub, _ = h.entitlementEngine.GetSubscriptionByProviderSubID(r.Context(), pay.PaymentLink)
			}
			if localSub == nil && pay.ExternalReference != "" {
				localSub, _ = h.entitlementEngine.GetSubscription(r.Context(), pay.ExternalReference)
			}

			if localSub != nil {
				localSub.PlanCode = "free"
				localSub.Status = "refunded"
				localSub.CancelAtPeriodEnd = false
				processingError = h.entitlementEngine.UpsertSubscription(r.Context(), localSub)
				log.Printf("Assinatura estornada/reembolsada para o usuário %s", localSub.UserID)
			}
		}

	case "PAYMENT_OVERDUE":
		if payloadData.Payment != nil {
			pay := payloadData.Payment
			var localSub *billing.Subscription
			if pay.Subscription != "" {
				localSub, _ = h.entitlementEngine.GetSubscriptionByProviderSubID(r.Context(), pay.Subscription)
			}
			if localSub == nil && pay.PaymentLink != "" {
				localSub, _ = h.entitlementEngine.GetSubscriptionByProviderSubID(r.Context(), pay.PaymentLink)
			}
			if localSub != nil {
				localSub.Status = "past_due"
				processingError = h.entitlementEngine.UpsertSubscription(r.Context(), localSub)
				log.Printf("Pagamento em atraso registrado para o usuário %s", localSub.UserID)
			}
		}

	case "SUBSCRIPTION_DELETED":
		if payloadData.Subscription != nil {
			asSub := payloadData.Subscription
			localSub, err := h.entitlementEngine.GetSubscriptionByProviderSubID(r.Context(), asSub.ID)
			if err == nil && localSub != nil {
				localSub.PlanCode = "free"
				localSub.Status = "canceled"
				localSub.CancelAtPeriodEnd = false
				processingError = h.entitlementEngine.UpsertSubscription(r.Context(), localSub)
			}
		}
	}

	var errStr *string
	if processingError != nil {
		errVal := processingError.Error()
		errStr = &errVal
		log.Printf("Erro ao processar regras do webhook Asaas: %v", processingError)
	}

	if err := h.entitlementEngine.MarkEventProcessed(r.Context(), billingEvent.ID, errStr); err != nil {
		log.Printf("Erro ao marcar evento Asaas como processado: %v", err)
	}

	if processingError != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao concluir regras de negócio do webhook")
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"processed"}`))
}
