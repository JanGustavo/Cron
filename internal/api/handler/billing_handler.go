package handler

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/JanGustavo/Cron/internal/api/middleware"
	"github.com/JanGustavo/Cron/internal/config"
	"github.com/JanGustavo/Cron/internal/domain/billing"
	"github.com/JanGustavo/Cron/internal/service"
	"github.com/stripe/stripe-go/v72"
	portalsession "github.com/stripe/stripe-go/v72/billingportal/session"
	checkoutsession "github.com/stripe/stripe-go/v72/checkout/session"
	"github.com/stripe/stripe-go/v72/customer"
	"github.com/stripe/stripe-go/v72/sub"
	"github.com/stripe/stripe-go/v72/webhook"
)

type BillingHandler struct {
	entitlementEngine *service.EntitlementEngine
	cfg               *config.Config
}

func NewBillingHandler(entitlementEngine *service.EntitlementEngine, cfg *config.Config) *BillingHandler {
	// Configura a chave privada da Stripe globalmente
	stripe.Key = cfg.StripeSecretKey
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

	successURL := req.SuccessURL
	if successURL == "" {
		successURL = h.cfg.FrontendURL + "/profile?success=true"
	}
	cancelURL := req.CancelURL
	if cancelURL == "" {
		cancelURL = h.cfg.FrontendURL + "/profile?canceled=true"
	}

	// Seleciona o Price ID conforme o período escolhido pelo usuário
	priceID := h.cfg.StripePriceIDProMonthly
	if req.Period == "yearly" {
		priceID = h.cfg.StripePriceIDProYearly
	}
	if priceID == "" {
		writeError(w, http.StatusInternalServerError, "Price ID do plano não configurado no servidor")
		return
	}

	params := &stripe.CheckoutSessionParams{
		PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
		Mode:               stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		ClientReferenceID: stripe.String(proj.UserID),
		SuccessURL:        stripe.String(successURL),
		CancelURL:         stripe.String(cancelURL),
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			Metadata: map[string]string{
				"user_id": proj.UserID,
			},
		},
	}
	params.AddMetadata("user_id", proj.UserID)

	sess, err := checkoutsession.New(params)
	if err != nil {
		log.Printf("Erro ao criar Checkout Session Stripe: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro ao criar sessão de checkout de pagamentos")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"url": sess.URL,
	})
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

	var customerID string
	if sub != nil && sub.ProviderCustomerID != nil && *sub.ProviderCustomerID != "" {
		customerID = *sub.ProviderCustomerID
	} else {
		// Se o usuário não possui Customer ID no Stripe (ex: criado manualmente/teste), cria no Stripe
		cParams := &stripe.CustomerParams{}
		cParams.AddMetadata("user_id", proj.UserID)
		c, err := customer.New(cParams)
		if err != nil {
			log.Printf("Erro ao registrar cliente no Stripe: %v", err)
			writeError(w, http.StatusBadRequest, "Usuário não possui uma assinatura ou cliente ativo no Stripe. Inicie a assinatura pelo botão Assinar PRO.")
			return
		}
		customerID = c.ID
		if sub != nil {
			sub.ProviderCustomerID = &customerID
			_ = h.entitlementEngine.UpsertSubscription(r.Context(), sub)
		}
	}

	returnURL := h.cfg.FrontendURL + "/profile"
	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(returnURL),
	}

	sess, err := portalsession.New(params)
	if err != nil {
		log.Printf("Erro ao criar Portal Session Stripe: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro ao criar portal de gerenciamento de assinaturas")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"url": sess.URL,
	})
}

// Webhook recebe, valida a assinatura e processa eventos emitidos pela Stripe
// POST /v1/billing/webhook
func (h *BillingHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	const MaxBodySize = 1048576 // 1MB
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodySize)
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Corpo da requisição muito longo ou inválido")
		return
	}

	sigHeader := r.Header.Get("Stripe-Signature")
	event, err := webhook.ConstructEvent(payload, sigHeader, h.cfg.StripeWebhookSecret)
	if err != nil {
		log.Printf("Erro ao validar assinatura do Webhook Stripe: %v", err)
		writeError(w, http.StatusBadRequest, "Assinatura de webhook inválida")
		return
	}

	// 1. Registro idempotente do evento
	billingEvent, isDuplicate, err := h.entitlementEngine.RegisterBillingEvent(
		r.Context(),
		"stripe",
		event.ID,
		string(event.Type),
		nil,
		payload,
	)
	if err != nil {
		log.Printf("Erro ao registrar evento de billing: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro interno ao processar transação")
		return
	}

	if isDuplicate {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ignored_duplicate"}`))
		return
	}

	var processingError error

	// 2. Roteamento de eventos financeiros
	switch event.Type {
	case "checkout.session.completed":
		var sess stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &sess); err == nil {
			userID := sess.ClientReferenceID
			if userID == "" {
				userID = sess.Metadata["user_id"]
			}

			if userID != "" && sess.Subscription != nil {
				// Busca dados de assinatura atualizados na Stripe
				stripeSub, err := sub.Get(sess.Subscription.ID, nil)
				if err == nil {
					processingError = h.processSubscriptionUpdate(r.Context(), userID, stripeSub)
				} else {
					processingError = err
				}
			}
		} else {
			processingError = err
		}

	case "customer.subscription.created", "customer.subscription.updated":
		var stripeSub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &stripeSub); err == nil {
			userID := stripeSub.Metadata["user_id"]
			if userID != "" {
				processingError = h.processSubscriptionUpdate(r.Context(), userID, &stripeSub)
			} else {
				// Se a assinatura não tiver metadata de user_id, pesquisa no banco pelo Customer ID
				existingSub, err := h.entitlementEngine.GetSubscriptionByProviderSubID(r.Context(), stripeSub.ID)
				if err == nil && existingSub != nil {
					processingError = h.processSubscriptionUpdate(r.Context(), existingSub.UserID, &stripeSub)
				} else {
					log.Printf("Aviso: evento %s ignorado (sem associação de user_id)", event.Type)
				}
			}
		} else {
			processingError = err
		}

	case "customer.subscription.deleted":
		var stripeSub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &stripeSub); err == nil {
			existingSub, err := h.entitlementEngine.GetSubscriptionByProviderSubID(r.Context(), stripeSub.ID)
			if err == nil && existingSub != nil {
				// Downgrade automático da conta do usuário para o plano Free
				existingSub.PlanCode = "free"
				existingSub.Status = "canceled"
				existingSub.CancelAtPeriodEnd = false
				
				pStart := time.Unix(stripeSub.CurrentPeriodStart, 0)
				pEnd := time.Unix(stripeSub.CurrentPeriodEnd, 0)
				existingSub.CurrentPeriodStart = &pStart
				existingSub.CurrentPeriodEnd = &pEnd

				processingError = h.entitlementEngine.UpsertSubscription(r.Context(), existingSub)
			}
		} else {
			processingError = err
		}
	}

	// 3. Finalização e persistência do status de processamento
	var errStr *string
	if processingError != nil {
		errVal := processingError.Error()
		errStr = &errVal
		log.Printf("Erro ao processar webhook da Stripe: %v", processingError)
	}

	if err := h.entitlementEngine.MarkEventProcessed(r.Context(), billingEvent.ID, errStr); err != nil {
		log.Printf("Erro ao marcar evento como processado: %v", err)
	}

	if processingError != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao concluir regras de negócio do webhook")
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"processed"}`))
}

func (h *BillingHandler) processSubscriptionUpdate(ctx context.Context, userID string, stripeSub *stripe.Subscription) error {
	pStart := time.Unix(stripeSub.CurrentPeriodStart, 0)
	pEnd := time.Unix(stripeSub.CurrentPeriodEnd, 0)

	sub := &billing.Subscription{
		UserID:                 userID,
		PlanCode:               "pro", // Preço e plano Pro no checkout
		Status:                 string(stripeSub.Status),
		BillingProvider:        "stripe",
		ProviderCustomerID:     &stripeSub.Customer.ID,
		ProviderSubscriptionID: &stripeSub.ID,
		CurrentPeriodStart:     &pStart,
		CurrentPeriodEnd:       &pEnd,
		CancelAtPeriodEnd:      stripeSub.CancelAtPeriodEnd,
	}

	return h.entitlementEngine.UpsertSubscription(ctx, sub)
}
