package project

import "time"

// Domain Entity: Project (Workspace).
// Responsabilidade: agrupar Jobs de um mesmo contexto/cliente.
// Um User pode ter N Projects. Cada Project tem sua própria API Key.
// O isolamento multi-tenant é garantido filtrando sempre por project_id.

type Project struct {
	ID            string
	UserID        string
	Name          string
	CreatedAt     time.Time
	WebhookSecret *string
}
