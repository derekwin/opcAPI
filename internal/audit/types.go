package audit

import "time"

type Event struct {
	ID             string            `json:"id"`
	Resource       string            `json:"resource"`
	Action         string            `json:"action"`
	ActorUserID    string            `json:"actor_user_id,omitempty"`
	ActorTenantID  string            `json:"actor_tenant_id,omitempty"`
	TargetID       string            `json:"target_id,omitempty"`
	TargetTenantID string            `json:"target_tenant_id,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
}

type RecordInput struct {
	Resource       string
	Action         string
	ActorUserID    string
	ActorTenantID  string
	TargetID       string
	TargetTenantID string
	Metadata       map[string]string
}

type Query struct {
	Resource       string
	Action         string
	ActorUserID    string
	TargetTenantID string
	Limit          int
	Offset         int
}

type Page struct {
	Data   []Event `json:"data"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}
