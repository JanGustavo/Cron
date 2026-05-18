package cronparser

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// parser é o parser padrão do robfig/cron — reutilizado em todas as chamadas.
// Criado uma vez aqui (package-level) por ser thread-safe e caro de instanciar.
var parser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
)

// Validate verifica se uma expression é válida antes de salvar no banco.
// Aceita cron UNIX (5 campos) ou o formato "every:Nm" (ex: "every:15m").
// Retorna erro descritivo se inválido — vai direto pro response da API.
func Validate(expr string) error {
	if isIntervalExpr(expr) {
		_, err := parseInterval(expr)
		return err
	}

	_, err := parser.Parse(expr)
	if err != nil {
		return fmt.Errorf("cron expression inválida: %w", err)
	}
	return nil
}
	
// NextRun calcula o próximo horário de execução a partir de now.
// É chamado pelo JobService na criação e pelo Scheduler após enfileirar.
// timezone deve ser um nome IANA válido: "America/Sao_Paulo", "UTC", etc.
func NextRun(expr, timezone string, now time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("timezone inválida %q: %w", timezone, err)
	}

	// Converte now para o timezone do job antes de calcular.
	// Sem isso, um job "todo dia às 9h em Brasília" calcularia errado se
	// o servidor estiver em UTC.
	nowInLoc := now.In(loc)

	if isIntervalExpr(expr) {
		duration, err := parseInterval(expr)
		if err != nil {
			return time.Time{}, err
		}
		return nowInLoc.Add(duration), nil
	}

	schedule, err := parser.Parse(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("erro ao parsear expression: %w", err)
	}

	return schedule.Next(nowInLoc), nil
}

// isIntervalExpr verifica se é o formato customizado "every:Nm".
func isIntervalExpr(expr string) bool {
	return strings.HasPrefix(expr, "every:")
}

// parseInterval converte "every:15m" → time.Duration.
// Unidades suportadas: m (minutos), h (horas).
// Limite mínimo: 1 minuto (regra de negócio do MVP).
func parseInterval(expr string) (time.Duration, error) {
	// Remove o prefixo "every:" e fica com "15m", "2h", etc
	raw := strings.TrimPrefix(expr, "every:")
	if raw == "" {
		return 0, fmt.Errorf("formato inválido: %q — use every:15m ou every:2h", expr)
	}

	unit := raw[len(raw)-1:]         // último caractere: "m" ou "h"
	valueStr := raw[:len(raw)-1]      // o restante: "15", "2", etc

	value, err := strconv.Atoi(valueStr)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("valor inválido em %q — deve ser um número positivo", expr)
	}

	switch unit {
	case "m":
		if value < 1 {
			return 0, fmt.Errorf("intervalo mínimo é 1 minuto")
		}
		return time.Duration(value) * time.Minute, nil
	case "h":
		return time.Duration(value) * time.Hour, nil
	default:
		return 0, fmt.Errorf("unidade desconhecida %q em %q — use 'm' (minutos) ou 'h' (horas)", unit, expr)
	}
}