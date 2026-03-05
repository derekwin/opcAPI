package billing

import "time"

type Balance struct {
	UserID    string    `json:"user_id"`
	Amount    float64   `json:"amount"`
	Currency  string    `json:"currency"`
	UpdatedAt time.Time `json:"updated_at"`

	TenantID string `json:"-"`
}

type Order struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Channel     string    `json:"channel"`
	Type        string    `json:"type"`
	PlanID      string    `json:"plan_id,omitempty"`
	Amount      float64   `json:"amount"`
	Currency    string    `json:"currency"`
	Status      string    `json:"status"`
	Description string    `json:"description,omitempty"`
	PeriodStart time.Time `json:"period_start,omitempty"`
	PeriodEnd   time.Time `json:"period_end,omitempty"`
	CreatedAt   time.Time `json:"created_at"`

	TenantID string `json:"-"`
}

type Subscription struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	PlanID      string    `json:"plan_id"`
	Status      string    `json:"status"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	CreatedAt   time.Time `json:"created_at"`

	TenantID string `json:"-"`
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
	Amount   float64 `json:"amount,omitempty"`   // legacy ignored
	Currency string  `json:"currency,omitempty"` // legacy ignored
	Months   int     `json:"months,omitempty"`   // optional; default 1
}

type UsagePolicy struct {
	PlanID             string `json:"plan_id"`
	RPMLimit           int    `json:"rpm_limit,omitempty"`
	FiveHourTokenLimit int    `json:"five_hour_token_limit,omitempty"`
	MonthlyTokenLimit  int    `json:"monthly_token_limit,omitempty"`
	Unlimited          bool   `json:"unlimited"`
}

type Membership struct {
	UserID      string    `json:"user_id"`
	PlanID      string    `json:"plan_id"`
	Status      string    `json:"status"` // free|active|expired
	PeriodStart time.Time `json:"period_start,omitempty"`
	PeriodEnd   time.Time `json:"period_end,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AdminSetMembershipRequest struct {
	PlanID      string    `json:"plan_id"`
	PeriodEnd   time.Time `json:"period_end,omitempty"`
	Description string    `json:"description,omitempty"`
}

type PlanConfig struct {
	PlanID             string    `json:"plan_id"`
	PriceCNY           float64   `json:"price_cny"`
	RPMLimit           int       `json:"rpm_limit,omitempty"`
	FiveHourTokenLimit int       `json:"five_hour_token_limit,omitempty"`
	MonthlyTokenLimit  int       `json:"monthly_token_limit,omitempty"`
	Unlimited          bool      `json:"unlimited"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type OrderQuery struct {
	ID      string
	UserID  string
	Type    string
	Status  string
	Channel string
	Limit   int
	Offset  int

	TenantID string
}

type OrderPage struct {
	Data   []Order `json:"data"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}
