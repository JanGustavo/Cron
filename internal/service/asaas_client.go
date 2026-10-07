package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/JanGustavo/Cron/internal/domain/billing"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type AsaasProvider interface {
	Payment(context.Context, string) (*billing.AsaasPayment, error)
	Subscription(context.Context, string) (*billing.AsaasSubscription, error)
	Link(context.Context, string) (*billing.AsaasLink, error)
	Payments(context.Context, string, string) ([]billing.AsaasPayment, error)
}
type AsaasClient struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}
type AsaasHTTPError struct{ Status int }

func (e AsaasHTTPError) Error() string { return fmt.Sprintf("Asaas HTTP %d", e.Status) }
func NewAsaasClient(key string, production bool) *AsaasClient {
	base := "https://api-sandbox.asaas.com/v3"
	if production && !strings.HasPrefix(key, "$aact_hmlg_") {
		base = "https://api.asaas.com/v3"
	}
	return &AsaasClient{BaseURL: base, APIKey: key, HTTP: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *AsaasClient) get(ctx context.Context, path string, out any) error {
	if c.APIKey == "" {
		return fmt.Errorf("Asaas API key missing")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("access_token", c.APIKey)
	req.Header.Set("User-Agent", "Cronflow/Asaas-Reconciliation")
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("Asaas request unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return AsaasHTTPError{resp.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil || len(raw) > 2*1024*1024 {
		return fmt.Errorf("Asaas response incomplete")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("Asaas response invalid")
	}
	return nil
}
func (c *AsaasClient) Payment(ctx context.Context, id string) (*billing.AsaasPayment, error) {
	p := &billing.AsaasPayment{}
	err := c.get(ctx, "/payments/"+url.PathEscape(id), p)
	if err == nil && p.ID != id {
		err = fmt.Errorf("Asaas payment identity mismatch")
	}
	return p, err
}
func (c *AsaasClient) Subscription(ctx context.Context, id string) (*billing.AsaasSubscription, error) {
	s := &billing.AsaasSubscription{}
	err := c.get(ctx, "/subscriptions/"+url.PathEscape(id), s)
	if err == nil && s.ID != id {
		err = fmt.Errorf("Asaas subscription identity mismatch")
	}
	return s, err
}
func (c *AsaasClient) Link(ctx context.Context, id string) (*billing.AsaasLink, error) {
	l := &billing.AsaasLink{}
	err := c.get(ctx, "/paymentLinks/"+url.PathEscape(id), l)
	if err == nil && l.ID != id {
		err = fmt.Errorf("Asaas link identity mismatch")
	}
	return l, err
}
func (c *AsaasClient) Payments(ctx context.Context, userID, subID string) ([]billing.AsaasPayment, error) {
	values := url.Values{"limit": {"100"}}
	if subID != "" {
		values.Set("subscription", subID)
	} else {
		values.Set("externalReference", userID)
	}
	var all []billing.AsaasPayment
	for page := 0; page < 20; page++ {
		values.Set("offset", strconv.Itoa(page*100))
		var result struct {
			Data    []billing.AsaasPayment `json:"data"`
			HasMore bool                   `json:"hasMore"`
		}
		if err := c.get(ctx, "/payments?"+values.Encode(), &result); err != nil {
			return nil, err
		}
		if result.Data == nil {
			return nil, fmt.Errorf("Asaas payment list missing")
		}
		all = append(all, result.Data...)
		if !result.HasMore {
			return all, nil
		}
		if len(result.Data) == 0 {
			return nil, fmt.Errorf("Asaas pagination incomplete")
		}
	}
	return nil, fmt.Errorf("Asaas pagination limit exceeded")
}
