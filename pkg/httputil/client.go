package httputil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"
)

// Result é o resultado de uma execução HTTP.
type Result struct {
	StatusCode int
	Body       string // truncado em 2KB
	DurationMs int
}

// isPrivateIP verifica se o IP pertence a redes locais, privadas ou reservadas.
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	// RFC 1918 (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16) + RFC 4193 (IPv6 local)
	if ip.IsPrivate() {
		return true
	}
	return false
}

var client = &http.Client{
	Timeout: 35 * time.Second, // margem acima do timeout máximo de 30s por job
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("httputil: limite de redirecionamentos (3) excedido")
		}
		return nil
	},
	Transport: &http.Transport{
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			Control: func(network, address string, c syscall.RawConn) error {
				if os.Getenv("ALLOW_LOCAL_WEBHOOKS") == "true" {
					return nil
				}
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				
				ip := net.ParseIP(host)
				if ip == nil {
					// Hostname — resolve e valida todos os IPs resolvidos
					ips, err := net.LookupIP(host)
					if err != nil {
						return fmt.Errorf("httputil: falha ao resolver hostname: %w", err)
					}
					for _, resolvedIP := range ips {
						if isPrivateIP(resolvedIP) {
							return fmt.Errorf("httputil SSRF: conexao bloqueada para IP local/privado: %s", resolvedIP)
						}
					}
					return nil
				}
				
				if isPrivateIP(ip) {
					return fmt.Errorf("httputil SSRF: conexao bloqueada para IP local/privado: %s", ip)
				}
				return nil
			},
		}).DialContext,
	},
}

// Execute dispara o HTTP request do job e retorna o resultado.
// Nunca retorna erro por status >= 400 — isso é decisão do Worker.
// Retorna erro apenas em falhas de rede, DNS ou timeout.
func Execute(ctx context.Context, method, url string, headers map[string]string, payload map[string]any, timeout time.Duration) (*Result, error) {
	start := time.Now()

	// Serializa o payload se existir
	var body io.Reader
	if payload != nil && (method == "POST" || method == "PUT" || method == "PATCH") {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("httputil.Execute: erro ao serializar payload: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("httputil.Execute: erro ao criar request: %w", err)
	}

	// Headers padrão
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "CronFlow/1.0")

	// Headers customizados do job sobrescrevem os padrão
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// Aplica timeout específico do job via context
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req = req.WithContext(timeoutCtx)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("httputil.Execute: request falhou: %w", err)
	}
	defer resp.Body.Close()

	durationMs := int(time.Since(start).Milliseconds())

	// Lê e trunca o body em 2KB
	rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024))

	return &Result{
		StatusCode: resp.StatusCode,
		Body:       string(rawBody),
		DurationMs: durationMs,
	}, nil
}
