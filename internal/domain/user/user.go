package user

import (
	"time"
)

// Domain Entity: User.
// Responsabilidade: representar o dono da conta.
// NÃO contém senha — autenticação é via API Key, não login/senha no MVP.

type Plan string 

const (
	PlanFree Plan = "free"
	PlanPaid Plan = "pro"
)

type User struct {
	ID                 string
	Email              string
	PasswordHash       string
	Plan               Plan
	FullName           string
	CPF                string
	EmailAlertsEnabled bool
	DailyDigestEnabled bool
	Timezone           string
	DigestHour         int
	LastDigestSentAt   *time.Time
	IsVerified         bool
	Role               string
	AiQueriesUsed      int
	CreatedAt          time.Time
}



func (u *User) MaxJobs(freePlanLimit, paidPlanLimit int) int {
	switch u.Plan {
	case PlanPaid:
		return paidPlanLimit
	default:
		return freePlanLimit
	}
}
