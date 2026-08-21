package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/JanGustavo/Cron/internal/config"
)

func main() {
	log.Println("🔍 Verificando disponibilidade dos modelos de IA...")

	cfg := config.Load()

	hasErrors := false

	if cfg.GeminiAPIKey == "" && cfg.GroqAPIKey == "" {
		log.Println("⚠️  AVISO: Nenhuma chave de API configurada para Gemini ou Groq no .env.")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if cfg.GeminiAPIKey != "" {
		log.Println("📡 Testando modelo Gemini (gemini-3.5-flash-lite)...")
		client := &http.Client{Timeout: 8 * time.Second}
		geminiUrl := "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.5-flash-lite?key=" + cfg.GeminiAPIKey

		req, err := http.NewRequestWithContext(ctx, "GET", geminiUrl, nil)
		if err != nil {
			log.Printf("❌ Erro ao criar requisição Gemini: %v\n", err)
			hasErrors = true
		} else {
			resp, err := client.Do(req)
			if err != nil {
				log.Printf("❌ Erro ao conectar na API do Gemini: %v\n", err)
				hasErrors = true
			} else {
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					var errBody map[string]any
					_ = json.NewDecoder(resp.Body).Decode(&errBody)
					errBytes, _ := json.Marshal(errBody)
					log.Printf("❌ API do Gemini indisponível (HTTP %d): %s\n", resp.StatusCode, string(errBytes))
					hasErrors = true
				} else {
					log.Println("✅ Gemini (gemini-3.5-flash-lite) está online e disponível!")
				}
			}
		}
	}

	if cfg.GroqAPIKey != "" {
		log.Println("📡 Testando fallback Groq (openai/gpt-oss-20b)...")
		client := &http.Client{Timeout: 8 * time.Second}
		groqUrl := "https://api.groq.com/openai/v1/models"

		req, err := http.NewRequestWithContext(ctx, "GET", groqUrl, nil)
		if err != nil {
			log.Printf("❌ Erro ao criar requisição Groq: %v\n", err)
			hasErrors = true
		} else {
			req.Header.Set("Authorization", "Bearer "+cfg.GroqAPIKey)
			resp, err := client.Do(req)
			if err != nil {
				log.Printf("❌ Erro ao conectar na API do Groq: %v\n", err)
				hasErrors = true
			} else {
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					var errBody map[string]any
					_ = json.NewDecoder(resp.Body).Decode(&errBody)
					errBytes, _ := json.Marshal(errBody)
					log.Printf("❌ API do Groq indisponível (HTTP %d): %s\n", resp.StatusCode, string(errBytes))
					hasErrors = true
				} else {
					var modelsResp struct {
						Data []struct {
							ID string `json:"id"`
						} `json:"data"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
						log.Printf("❌ Erro ao decodificar resposta do Groq: %v\n", err)
						hasErrors = true
					} else {
						found := false
						var availableModels []string
						for _, m := range modelsResp.Data {
							availableModels = append(availableModels, m.ID)
							if m.ID == "openai/gpt-oss-20b" {
								found = true
							}
						}
						if found {
							log.Println("✅ Groq (openai/gpt-oss-20b) está online e disponível!")
						} else {
							log.Printf("❌ Modelo 'openai/gpt-oss-20b' não encontrado no Groq. Modelos disponíveis: %s\n", strings.Join(availableModels, ", "))
							hasErrors = true
						}
					}
				}
			}
		}
	}

	if hasErrors {
		log.Println("⚠️  Atenção: A configuração de IA possui erros. O chat pode falhar em execução.")
		for _, arg := range os.Args {
			if arg == "--strict" {
				os.Exit(1)
			}
		}
	} else {
		log.Println("🎉 Configuração de IA validada com sucesso!")
	}
}
