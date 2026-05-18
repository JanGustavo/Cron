package cronparser_test

import (
	"testing"
	"time"

	"github.com/JanGustavo/Cron/pkg/cronparser"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		expr    string
		wantErr bool
	}{
		{"0 9 * * 1", false},      // toda segunda às 9h — válido
		{"*/5 * * * *", false},    // a cada 5 minutos — válido
		{"every:15m", false},      // intervalo 15min — válido
		{"every:2h", false},       // intervalo 2h — válido
		{"every:0m", true},        // zero minutos — inválido
		{"every:abc", true},       // não é número — inválido
		{"every:", true},          // sem valor — inválido
		{"99 * * * *", true},      // minuto 99 não existe — inválido
		{"isso nao e cron", true}, // lixo — inválido
	}

	for _, c := range cases {
		err := cronparser.Validate(c.expr)
		if (err != nil) != c.wantErr {
			t.Errorf("Validate(%q): wantErr=%v, got err=%v", c.expr, c.wantErr, err)
		}
	}
}

func TestNextRun(t *testing.T) {
	// Referência: segunda-feira, 10 de março de 2025, às 08:00 UTC
	now := time.Date(2025, 3, 10, 8, 0, 0, 0, time.UTC)

	t.Run("every:30m adiciona 30 minutos", func(t *testing.T) {
		next, err := cronparser.NextRun("every:30m", "UTC", now)
		if err != nil {
			t.Fatal(err)
		}
		expected := now.Add(30 * time.Minute)
		if !next.Equal(expected) {
			t.Errorf("esperado %v, got %v", expected, next)
		}
	})

	t.Run("cron diário calcula próxima ocorrência", func(t *testing.T) {
		// "0 9 * * *" = todo dia às 9h. São 8h agora, então deve retornar 9h hoje.
		next, err := cronparser.NextRun("0 9 * * *", "UTC", now)
		if err != nil {
			t.Fatal(err)
		}
		if next.Hour() != 9 || next.Minute() != 0 {
			t.Errorf("esperado 09:00, got %v", next)
		}
	})

	t.Run("timezone America/Sao_Paulo", func(t *testing.T) {
		next, err := cronparser.NextRun("every:1h", "America/Sao_Paulo", now)
		if err != nil {
			t.Fatal(err)
		}
		if next.IsZero() {
			t.Error("next não deve ser zero")
		}
	})

	t.Run("timezone inválida retorna erro", func(t *testing.T) {
		_, err := cronparser.NextRun("every:5m", "Narnia/Capital", now)
		if err == nil {
			t.Error("esperado erro para timezone inválida")
		}
	})
}