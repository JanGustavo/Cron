package user

// Domain Entity: User.
// Responsabilidade: representar o dono da conta.
// NÃO contém senha — autenticação é via API Key, não login/senha no MVP.

type Plan string

const (
	PlanFree Plan = "free"
	PlanPaid Plan = "paid"
)

type User struct {
	ID    string
	Email string
	Plan  Plan
}
