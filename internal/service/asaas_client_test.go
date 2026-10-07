package service

import (
	"context"
	"github.com/JanGustavo/Cron/internal/domain/billing"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type asaasRoundTrip func(*http.Request) (*http.Response, error)

func (f asaasRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestAsaasPaidPeriodStableAcrossSettlement(t *testing.T) {
	cases := []struct{ name, client, due, cycle, end string }{
		{"month end", "2026-01-31", "2026-01-31", "MONTHLY", "2026-02-28"},
		{"leap monthly", "2028-01-31", "2028-01-31", "MONTHLY", "2028-02-29"},
		{"leap yearly", "2028-02-29", "2028-02-29", "YEARLY", "2029-02-28"},
		{"early payment", "2026-01-02", "2026-01-07", "MONTHLY", "2026-02-07"},
		{"late renewal", "2026-01-09", "2026-01-07", "MONTHLY", "2026-02-09"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := billing.AsaasPayment{ClientPaymentDate: tc.client, DueDate: tc.due, PaymentDate: "2030-01-01"}
			_, end, err := AsaasPaidPeriod(p, tc.cycle)
			if err != nil || end.Format("2006-01-02") != tc.end {
				t.Fatalf("%s %v", end, err)
			}
		})
	}
	for _, p := range []billing.AsaasPayment{{}, {ClientPaymentDate: "bad"}, {ClientPaymentDate: "2026-01-01", DueDate: "bad"}} {
		if _, _, err := AsaasPaidPeriod(p, "MONTHLY"); err == nil {
			t.Fatal("invalid dates accepted")
		}
	}
	if _, _, err := AsaasPaidPeriod(billing.AsaasPayment{ClientPaymentDate: "2026-01-01"}, ""); err == nil {
		t.Fatal("missing cycle inferred")
	}
}
func TestAsaasClientReadOnlyAndCompletePagination(t *testing.T) {
	calls := 0
	c := NewAsaasClient("test-key", false)
	c.HTTP = &http.Client{Timeout: time.Second, Transport: asaasRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.Header.Get("access_token") != "test-key" || r.Body != nil {
			t.Fatal("unsafe provider request")
		}
		calls++
		body := `{"data":[{"id":"second"}],"hasMore":false}`
		if r.URL.Query().Get("offset") == "0" {
			body = `{"data":[{"id":"first"}],"hasMore":true}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	payments, err := c.Payments(context.Background(), "user", "")
	if err != nil || len(payments) != 2 || calls != 2 {
		t.Fatalf("incomplete payments %v %v", payments, err)
	}
	c.HTTP.Transport = asaasRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"hasMore":false}`)), Header: make(http.Header)}, nil
	})
	if _, err := c.Payments(context.Background(), "user", ""); err == nil {
		t.Fatal("missing data accepted as empty history")
	}
	c.HTTP.Transport = asaasRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader(`secret`)), Header: make(http.Header)}, nil
	})
	if _, err := c.Payment(context.Background(), "pay"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("provider error missing or exposed body")
	}
}
