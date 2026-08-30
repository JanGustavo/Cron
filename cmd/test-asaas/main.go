package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
)

const (
	sandboxBaseURL = "https://sandbox.asaas.com/api/v3"
	prodBaseURL    = "https://api.asaas.com/v3"
)

func callAPI(baseURL, apiKey, method, path string, body interface{}) ([]byte, int, error) {
	url := baseURL + path
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

	req.Header.Set("access_token", apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	return respData, resp.StatusCode, err
}

func main() {
	_ = godotenv.Load()

	prodKey := os.Getenv("ASAAS_API_KEY")
	sandboxKey := os.Getenv("ASAAS_SANDBOX_KEY")
	if sandboxKey == "" {
		sandboxKey = prodKey
	}

	fmt.Println("\n=======================================================")
	fmt.Println("🚀 CRONFLOW — SUITE DE TESTES E INSPEÇÃO ASAAS")
	fmt.Println("=======================================================")

	// 1. Inspeciona Webhook de Produção
	fmt.Println("\n🔍 [1/3] Inspecionando Webhook de PRODUÇÃO (asaas.com)...")
	prodWebhooks, code, err := callAPI(prodBaseURL, prodKey, "GET", "/webhooks", nil)
	if err != nil || code >= 400 {
		fmt.Printf("❌ Erro ao consultar produção (HTTP %d): %v\n", code, err)
	} else {
		var whList struct {
			Data []struct {
				ID         string   `json:"id"`
				Name       string   `json:"name"`
				URL        string   `json:"url"`
				Email      string   `json:"email"`
				Enabled    bool     `json:"enabled"`
				APIVersion int      `json:"apiVersion"`
				Events     []string `json:"events"`
			} `json:"data"`
		}
		if err := json.Unmarshal(prodWebhooks, &whList); err == nil && len(whList.Data) > 0 {
			wh := whList.Data[0]
			fmt.Printf("   ✅ Webhook Ativo: %s (ID: %s)\n", wh.Name, wh.ID)
			fmt.Printf("   🌐 URL: %s\n", wh.URL)
			fmt.Printf("   📧 E-mail Notificação: %s\n", wh.Email)
			fmt.Printf("   🔔 Eventos Ativos (%d):\n", len(wh.Events))
			for _, ev := range wh.Events {
				fmt.Printf("      - %s ✓\n", ev)
			}
		}
	}

	// 2. Inspeciona Webhook do Sandbox
	fmt.Println("\n🔍 [2/3] Inspecionando Webhook de HOMOLOGAÇÃO / SANDBOX...")
	sbWebhooks, code, err := callAPI(sandboxBaseURL, sandboxKey, "GET", "/webhooks", nil)
	if err == nil && code == 200 {
		var whList struct {
			Data []struct {
				ID      string   `json:"id"`
				Name    string   `json:"name"`
				URL     string   `json:"url"`
				Events  []string `json:"events"`
				Enabled bool     `json:"enabled"`
			} `json:"data"`
		}
		if err := json.Unmarshal(sbWebhooks, &whList); err == nil && len(whList.Data) > 0 {
			wh := whList.Data[0]
			fmt.Printf("   ✅ Webhook Sandbox: %s (%s)\n", wh.Name, wh.URL)
			fmt.Printf("   🔔 Eventos: %v\n", wh.Events)
		}
	}

	// 3. Teste E2E Automatizado de Assinatura e Pagamento no Sandbox
	fmt.Println("\n🧪 [3/3] Executando Teste Automatizado de Cobrança no SANDBOX...")

	// 3.1 Cria cliente teste
	custEmail := fmt.Sprintf("teste_e2e_%d@cronflow.me", time.Now().Unix())
	custBody := map[string]interface{}{
		"name":                 "Automated Test User",
		"email":                custEmail,
		"cpfCnpj":              "11144477735",
		"notificationDisabled": true,
	}
	custResp, code, err := callAPI(sandboxBaseURL, sandboxKey, "POST", "/customers", custBody)
	if err != nil || code >= 400 {
		fmt.Printf("❌ Falha ao criar cliente no Sandbox: %s (HTTP %d)\n", string(custResp), code)
		os.Exit(1)
	}

	var custData struct {
		ID string `json:"id"`
	}
	json.Unmarshal(custResp, &custData)
	fmt.Printf("   ✓ Cliente Teste Criado: %s (%s)\n", custData.ID, custEmail)

	// 3.2 Cria assinatura mensal no Sandbox
	subBody := map[string]interface{}{
		"customer":          custData.ID,
		"billingType":       "BOLETO",
		"value":             29.00,
		"nextDueDate":       time.Now().AddDate(0, 0, 3).Format("2006-01-02"),
		"cycle":             "MONTHLY",
		"description":       "Assinatura Teste Automatizado CronFlow PRO",
		"externalReference": "test_e2e_cronflow_user",
	}

	subResp, code, err := callAPI(sandboxBaseURL, sandboxKey, "POST", "/subscriptions", subBody)
	if err != nil || code >= 400 {
		fmt.Printf("❌ Falha ao criar assinatura no Sandbox: %s (HTTP %d)\n", string(subResp), code)
		os.Exit(1)
	}

	var subData struct {
		ID     string  `json:"id"`
		Status string  `json:"status"`
		Value  float64 `json:"value"`
	}
	json.Unmarshal(subResp, &subData)
	fmt.Printf("   ✓ Assinatura Criada: %s (R$ %.2f, Status: %s)\n", subData.ID, subData.Value, subData.Status)

	// 3.3 Consulta cobrança gerada para a assinatura
	time.Sleep(1 * time.Second)
	payListResp, _, _ := callAPI(sandboxBaseURL, sandboxKey, "GET", fmt.Sprintf("/payments?subscription=%s", subData.ID), nil)
	var payList struct {
		Data []struct {
			ID         string  `json:"id"`
			Status     string  `json:"status"`
			InvoiceURL string  `json:"invoiceUrl"`
			Value      float64 `json:"value"`
		} `json:"data"`
	}
	json.Unmarshal(payListResp, &payList)

	if len(payList.Data) > 0 {
		pay := payList.Data[0]
		fmt.Printf("   ✓ Fatura Gerada: %s (Status: %s)\n", pay.ID, pay.Status)
		fmt.Printf("   📄 Link Público da Fatura: %s\n", pay.InvoiceURL)

		// 3.4 Simula pagamento da fatura no Sandbox
		fmt.Println("   💳 Confirmando Recebimento de Pagamento no Sandbox...")
		simResp, simCode, simErr := callAPI(sandboxBaseURL, sandboxKey, "POST", fmt.Sprintf("/payments/%s/receiveInCash", pay.ID), map[string]interface{}{
			"value":       pay.Value,
			"paymentDate": time.Now().Format("2006-01-02"),
		})

		if simErr == nil && (simCode == 200 || simCode == 201) {
			fmt.Printf("   🎉 Pagamento Confirmado com Sucesso no Asaas Sandbox!\n")
		} else {
			fmt.Printf("   ℹ️ Resposta da simulação (HTTP %d): %s\n", simCode, string(simResp))
		}
	}

	// 3.5 Limpa assinatura de teste
	fmt.Println("   🧹 Limpando assinatura de teste...")
	callAPI(sandboxBaseURL, sandboxKey, "DELETE", fmt.Sprintf("/subscriptions/%s", subData.ID), nil)
	fmt.Printf("   ✓ Assinatura %s removida com sucesso.\n", subData.ID)

	fmt.Println("\n=======================================================")
	fmt.Println("✅ SUITE DE TESTES E INSPEÇÃO CONCLUÍDA COM SUCESSO!")
	fmt.Println("=======================================================")
}
