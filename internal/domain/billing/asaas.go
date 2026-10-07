package billing

import "time"

type AsaasEvent struct {
	ID           string             `json:"id"`
	Event        string             `json:"event"`
	DateCreated  string             `json:"dateCreated"`
	Payment      *AsaasPayment      `json:"payment,omitempty"`
	Subscription *AsaasSubscription `json:"subscription,omitempty"`
}
type AsaasPayment struct {
	Value             float64 `json:"value"`
	BillingType       string  `json:"billingType"`
	ID                string  `json:"id"`
	Customer          string  `json:"customer"`
	Subscription      string  `json:"subscription"`
	PaymentLink       string  `json:"paymentLink"`
	Status            string  `json:"status"`
	ExternalReference string  `json:"externalReference"`
	DueDate           string  `json:"dueDate"`
	ClientPaymentDate string  `json:"clientPaymentDate"`
	ConfirmedDate     string  `json:"confirmedDate"`
	PaymentDate       string  `json:"paymentDate"`
	Deleted           bool    `json:"deleted"`
}
type AsaasSubscription struct {
	ID          string `json:"id"`
	Customer    string `json:"customer"`
	PaymentLink string `json:"paymentLink"`
	Status      string `json:"status"`
	Cycle       string `json:"cycle"`
	Deleted     bool   `json:"deleted"`
}
type AsaasLink struct {
	ID    string `json:"id"`
	Cycle string `json:"subscriptionCycle"`
}
type AsaasAccount struct {
	UserID, SubscriptionID, Cycle string
	Links                         map[string]string
}
type ProviderSnapshot struct {
	AccountID         string
	SubscriptionID    string
	CustomerID        string
	Cycle             string
	Status            string
	PlanCode          string
	Start, End        *time.Time
	CancelAtPeriodEnd bool
	CheckedAt         time.Time
}
