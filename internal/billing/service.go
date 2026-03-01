package billing

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

type Service struct {
	store  *FileStore
	logger *slog.Logger

	inputTokenPrice  float64
	outputTokenPrice float64
}

func NewService(store *FileStore, logger *slog.Logger) *Service {
	return &Service{
		store:            store,
		logger:           logger,
		inputTokenPrice:  0.000002,
		outputTokenPrice: 0.000004,
	}
}

func (s *Service) GetBalance(_ context.Context, tenantID string) (Balance, error) {
	if s.store == nil {
		return Balance{}, ErrStoreUnavailable
	}
	return s.store.GetOrCreateBalance(tenantID, "CNY")
}

func (s *Service) Topup(_ context.Context, tenantID string, req TopupRequest) (Balance, Order, error) {
	if s.store == nil {
		return Balance{}, Order{}, ErrStoreUnavailable
	}
	if req.Amount <= 0 {
		return Balance{}, Order{}, ErrInvalidAmount
	}
	if !isSupportedChannel(req.Channel) {
		return Balance{}, Order{}, ErrInvalidChannel
	}
	currency := normalizeCurrency(req.Currency)
	if currency == "" {
		return Balance{}, Order{}, ErrInvalidCurrency
	}

	order := Order{
		ID:          newID("ord_"),
		TenantID:    tenantID,
		Channel:     strings.ToLower(strings.TrimSpace(req.Channel)),
		Type:        "topup",
		Amount:      req.Amount,
		Currency:    currency,
		Status:      "paid",
		Description: strings.TrimSpace(req.Description),
		CreatedAt:   time.Now().UTC(),
	}

	order, err := s.store.CreateOrder(order)
	if err != nil {
		return Balance{}, Order{}, err
	}

	bal, err := s.store.AddBalance(tenantID, req.Amount, currency)
	if err != nil {
		return Balance{}, Order{}, err
	}

	if s.logger != nil {
		s.logger.Info("billing topup", "tenant_id", tenantID, "amount", req.Amount, "currency", currency, "order_id", order.ID)
	}
	return bal, order, nil
}

func (s *Service) Subscribe(_ context.Context, tenantID string, req SubscribeRequest) (Subscription, Order, error) {
	if s.store == nil {
		return Subscription{}, Order{}, ErrStoreUnavailable
	}
	if req.PlanID == "" {
		return Subscription{}, Order{}, ErrInvalidPlan
	}
	if req.Amount <= 0 {
		return Subscription{}, Order{}, ErrInvalidAmount
	}
	if req.Months <= 0 {
		return Subscription{}, Order{}, ErrInvalidSubscription
	}
	if !isSupportedChannel(req.Channel) {
		return Subscription{}, Order{}, ErrInvalidChannel
	}
	currency := normalizeCurrency(req.Currency)
	if currency == "" {
		return Subscription{}, Order{}, ErrInvalidCurrency
	}

	now := time.Now().UTC()
	sub := Subscription{
		ID:          newID("sub_"),
		TenantID:    tenantID,
		PlanID:      strings.TrimSpace(req.PlanID),
		Status:      "active",
		PeriodStart: now,
		PeriodEnd:   now.AddDate(0, req.Months, 0),
		CreatedAt:   now,
	}
	order := Order{
		ID:        newID("ord_"),
		TenantID:  tenantID,
		Channel:   strings.ToLower(strings.TrimSpace(req.Channel)),
		Type:      "subscription",
		Amount:    req.Amount,
		Currency:  currency,
		Status:    "paid",
		CreatedAt: now,
	}

	sub, err := s.store.CreateSubscription(sub)
	if err != nil {
		return Subscription{}, Order{}, err
	}
	order, err = s.store.CreateOrder(order)
	if err != nil {
		return Subscription{}, Order{}, err
	}

	if s.logger != nil {
		s.logger.Info("billing subscribe", "tenant_id", tenantID, "plan_id", sub.PlanID, "order_id", order.ID)
	}
	return sub, order, nil
}

func (s *Service) ChargeUsage(_ context.Context, tenantID string, promptTokens, completionTokens int) (Balance, float64, error) {
	if s.store == nil {
		return Balance{}, 0, ErrStoreUnavailable
	}
	if promptTokens < 0 || completionTokens < 0 {
		return Balance{}, 0, ErrInvalidUsage
	}
	if promptTokens == 0 && completionTokens == 0 {
		bal, err := s.store.GetOrCreateBalance(tenantID, "CNY")
		return bal, 0, err
	}

	bal, err := s.store.GetOrCreateBalance(tenantID, "CNY")
	if err != nil {
		return Balance{}, 0, err
	}

	cost := float64(promptTokens)*s.inputTokenPrice + float64(completionTokens)*s.outputTokenPrice
	if cost <= 0 {
		return bal, 0, nil
	}

	updated, err := s.store.DeductBalance(tenantID, cost, bal.Currency)
	if err != nil {
		return Balance{}, cost, err
	}

	if s.logger != nil {
		s.logger.Info("billing usage charged",
			"tenant_id", tenantID,
			"prompt_tokens", promptTokens,
			"completion_tokens", completionTokens,
			"cost", cost,
			"currency", updated.Currency,
		)
	}
	return updated, cost, nil
}

func (s *Service) ListOrders(ctx context.Context, tenantID string, limit int) ([]Order, error) {
	page, err := s.ListOrdersAdmin(ctx, OrderQuery{TenantID: tenantID, Limit: limit})
	if err != nil {
		return nil, err
	}
	return page.Data, nil
}

func (s *Service) ListOrdersAdmin(_ context.Context, query OrderQuery) (OrderPage, error) {
	if s.store == nil {
		return OrderPage{}, ErrStoreUnavailable
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}

	orders, total, err := s.store.QueryOrders(OrderQuery{
		TenantID: strings.TrimSpace(query.TenantID),
		Type:     strings.ToLower(strings.TrimSpace(query.Type)),
		Status:   strings.ToLower(strings.TrimSpace(query.Status)),
		Channel:  strings.ToLower(strings.TrimSpace(query.Channel)),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		return OrderPage{}, err
	}

	return OrderPage{
		Data:   orders,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func isSupportedChannel(ch string) bool {
	switch strings.ToLower(strings.TrimSpace(ch)) {
	case "stripe", "yipay", "manual":
		return true
	default:
		return false
	}
}

func normalizeCurrency(c string) string {
	switch strings.ToUpper(strings.TrimSpace(c)) {
	case "CNY", "USD":
		return strings.ToUpper(strings.TrimSpace(c))
	default:
		return ""
	}
}
