package cronparser

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// O parser padrão que a gente vai usar pro projeto todo.
// Usamos o formato tradicional (5 campos) pra ficar o mais compatível possível 
// com o crontab do linux. O Descriptor é pra suportar @hourly, @daily, etc.
var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// Validate checa se a string do cron tá no formato certo.
// É super importante validar antes de salvar no banco pra evitar 
// que o worker crashe depois na hora de tentar agendar.
func Validate(expr string) error {
	_, err := parser.Parse(expr)
	if err != nil {
		return fmt.Errorf("cron inválido: %w", err)
	}
	return nil
}

// NextRun calcula a próxima vez que o job vai rodar baseado na expressão cron e no fuso.
// Isso é útil pra mostrar na UI ou pro próprio core do scheduler saber quando acordar.
func NextRun(expr, timezone string) (time.Time, error) {
	schedule, err := parser.Parse(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("expressão cron não bate com o padrão: %w", err)
	}

	// Carrega o fuso pra evitar dores de cabeça com horário de verão e afins
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("deu ruim ao carregar o timezone (%s): %w", timezone, err)
	}

	// Calcula a partir do momento atual lá no fuso especificado
	now := time.Now().In(loc)
	return schedule.Next(now), nil
}

// ParseInterval tenta dar uma facilitada com um parser mais "humano".
// O front tinha pedido pra gente suportar uns atalhos tipo "every:15m" 
// ao invés de sempre ter que mandar "*/15 * * * *".
func ParseInterval(input string) (string, error) {
	// Se já for uma macro que o pacote entende de fábrica (tipo @hourly), passa direto
	if strings.HasPrefix(input, "@") {
		return input, nil
	}

	// Lida com a nossa sintaxe inventada (every:XXm ou every:XXh)
	if strings.HasPrefix(input, "every:") {
		val := strings.TrimPrefix(input, "every:")
		
		if strings.HasSuffix(val, "m") {
			mins := strings.TrimSuffix(val, "m")
			return fmt.Sprintf("*/%s * * * *", mins), nil
		}
		
		if strings.HasSuffix(val, "h") {
			hours := strings.TrimSuffix(val, "h")
			return fmt.Sprintf("0 */%s * * *", hours), nil
		}

		return "", fmt.Errorf("o atalho '%s' tá num formato que a gente ainda não suporta", input)
	}

	// Se não for o nosso atalho, assume que já é o cron em si e devolve puro
	return input, nil
}
