package billing

import "time"

type Balance struct {
	TenantID  string    `json:"tenant_id"`
	Amount    float64   `json:"amount"`
	Currency  string    `json:"currency"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Order struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Channel     string    `json:"channel"`
	Type        string    `json:"type"`
	Amount      float64   `json:"amount"`
	Currency    string    `json:"currency"`
	Status      string    `json:"status"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type Subscription struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	PlanID      string    `json:"plan_id"`
	Status      string    `json:"status"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	CreatedAt   time.Time `json:"created_at"`
}

type TopupRequest struct {
	Channel     string  `json:"channel"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	Description string  `json:"description,omitempty"`
}

type SubscribeRequest struct {
	PlanID   string  `json:"plan_id"`
	Channel  string  `json:"channel"`
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
	Months   int     `json:"months"`
}

type OrderQuery struct {
	TenantID string
	Type     string
	Status   string
	Channel  string
	Limit    int
	Offset   int
}

type OrderPage struct {
	Data   []Order `json:"data"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}
