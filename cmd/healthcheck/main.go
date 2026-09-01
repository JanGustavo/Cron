package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/JanGustavo/Cron/cmd/healthcheck/checks"
	"github.com/JanGustavo/Cron/cmd/healthcheck/config"
	"github.com/JanGustavo/Cron/cmd/healthcheck/notifier"
	"github.com/JanGustavo/Cron/cmd/healthcheck/report"
)

func main() {
	cfg := config.Load()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("[HealthCheck] Sinal de shutdown recebido")
		cancel()
	}()

	var wg sync.WaitGroup

	if cfg.Server.Enabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runServer(ctx, cfg)
		}()
	}

	if cfg.Schedule.Enabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runScheduler(ctx, cfg)
		}()
	}

	if cfg.RunOnce {
		runOnce(ctx, cfg)
	}

	wg.Wait()
	log.Println("[HealthCheck] Finalizado")
}

func runServer(ctx context.Context, cfg *config.Config) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		results := runAllChecks(ctx, cfg)
		w.Header().Set("Content-Type", "application/json")
		if results.HasFailures() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		json.NewEncoder(w).Encode(results)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	log.Printf("[HealthCheck] Servidor HTTP em :%d", cfg.Server.Port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Printf("[HealthCheck] Erro no servidor: %v", err)
	}
}

func runScheduler(ctx context.Context, cfg *config.Config) {
	ticker := time.NewTicker(cfg.Schedule.Interval)
	defer ticker.Stop()

	runAllChecks(ctx, cfg)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runAllChecks(ctx, cfg)
		}
	}
}

func runOnce(ctx context.Context, cfg *config.Config) {
	results := runAllChecks(ctx, cfg)
	output, _ := json.MarshalIndent(results, "", "  ")
	fmt.Println(string(output))

	if results.HasFailures() && !cfg.AllowFailures {
		os.Exit(1)
	}
}

func runAllChecks(ctx context.Context, cfg *config.Config) *report.Report {
	start := time.Now()
	log.Println("[HealthCheck] Iniciando suite de verificação...")

	runner := checks.NewRunner(cfg)
	results := runner.Run(ctx)

	results.Duration = time.Since(start)
	results.Timestamp = time.Now().UTC()

	notifier.Send(cfg, results)
	report.Save(cfg, results)

	log.Printf("[HealthCheck] Concluído em %v — %d/%d passed",
		results.Duration, results.Passed, results.Total)
	return results
}