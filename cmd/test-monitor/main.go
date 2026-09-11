package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	baseURL := os.Getenv("CRONFLOW_URL")
	if baseURL == "" {
		baseURL = "https://cronflow.app"
	}
	apiKey := os.Getenv("CRONFLOW_API_KEY")

	fmt.Printf("=== 🔍 Testando Monitoramento de 'goroutines_count' no CronFlow ===\n")
	fmt.Printf("URL Base: %s\n\n", baseURL)

	// 1. Consultar métricas do sistema
	fmt.Println("1. Consultando GET /v1/metrics/system...")
	resp, err := http.Get(baseURL + "/v1/metrics/system")
	if err != nil {
		fmt.Printf("Erro ao buscar métricas: %v\n", err)
		return
	}
	defer resp.Body.Close()
	metricsBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("Resposta do Sistema: %s\n\n", string(metricsBody))

	var metrics map[string]interface{}
	_ = json.Unmarshal(metricsBody, &metrics)

	goroutineCount := 8
	if val, ok := metrics["goroutines_count"].(float64); ok {
		goroutineCount = int(val)
	}

	// 2. Se houver API Key, cadastra a regra e testa o check
	if apiKey == "" {
		fmt.Println("ℹ️ Nenhuma CRONFLOW_API_KEY informada no ambiente.")
		fmt.Println("Para testar com autenticação na API de produção, execute:")
		fmt.Println("CRONFLOW_API_KEY=\"cf_live_...\" go run ./cmd/test-monitor")
		return
	}

	// 3. Criar regra de monitoramento
	fmt.Println("2. Cadastrando regra: goroutines_count <= 20...")
	rulePayload := map[string]interface{}{
		"name":            "Alerta de Goroutines Excessivas",
		"key":             "goroutines_count",
		"operator":        "lte",
		"threshold_value": "20",
		"alert_email":     true,
	}
	ruleBytes, _ := json.Marshal(rulePayload)
	req, _ := http.NewRequest("POST", baseURL+"/v1/monitor/rules", bytes.NewReader(ruleBytes))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	ruleResp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("Erro ao criar regra: %v\n", err)
		return
	}
	defer ruleResp.Body.Close()
	ruleBody, _ := io.ReadAll(ruleResp.Body)
	fmt.Printf("Status Criar Regra: %d — Resposta: %s\n\n", ruleResp.StatusCode, string(ruleBody))

	// 4. Ingerir valor da goroutines_count e avaliar
	fmt.Printf("3. Ingerindo valor goroutines_count = %d...\n", goroutineCount)
	checkPayload := map[string]interface{}{
		"key":   "goroutines_count",
		"value": goroutineCount,
	}
	checkBytes, _ := json.Marshal(checkPayload)
	checkReq, _ := http.NewRequest("POST", baseURL+"/v1/monitor/check", bytes.NewReader(checkBytes))
	checkReq.Header.Set("Authorization", "Bearer "+apiKey)
	checkReq.Header.Set("Content-Type", "application/json")

	checkResp, err := http.DefaultClient.Do(checkReq)
	if err != nil {
		fmt.Printf("Erro ao verificar payload: %v\n", err)
		return
	}
	defer checkResp.Body.Close()
	checkResBody, _ := io.ReadAll(checkResp.Body)
	fmt.Printf("Status Check: %d — Resultado: %s\n", checkResp.StatusCode, string(checkResBody))
}
