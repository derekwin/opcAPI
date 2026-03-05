package billing

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"
)

type Service struct {
	store  *FileStore
	logger *slog.Logger
}

const settlementCurrency = "CNY"

var defaultPlanCatalog = map[string]PlanConfig{
	"free": {
		PlanID:             "free",
		PriceCNY:           0,
		RPMLimit:           60,
		FiveHourTokenLimit: 100000,
		MonthlyTokenLimit:  500000,
		Unlimited:          false,
	},
	"starter": {
		PlanID:             "starter",
		PriceCNY:           29,
		RPMLimit:           180,
		FiveHourTokenLimit: 600000,
		MonthlyTokenLimit:  3000000,
		Unlimited:          false,
	},
	"pro": {
		PlanID:             "pro",
		PriceCNY:           99,
		RPMLimit:           480,
		FiveHourTokenLimit: 3000000,
		MonthlyTokenLimit:  15000000,
		Unlimited:          false,
	},
	"business": {
		PlanID:             "business",
		PriceCNY:           299,
		RPMLimit:           0,
		FiveHourTokenLimit: 0,
		MonthlyTokenLimit:  0,
		Unlimited:          true,
	},
}

func NewService(store *FileStore, logger *slog.Logger) *Service {
	return &Service{store: store, logger: logger}
}

func (s *Service) GetBalance(_ context.Context, userID string) (Balance, error) {
	if s.store == nil {
		return Balance{}, ErrStoreUnavailable
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return Balance{}, ErrInvalidUserID
	}
	return s.store.GetOrCreateBalance(userID, settlementCurrency)
}

func (s *Service) ListBalances(_ context.Context) ([]Balance, error) {
	if s.store == nil {
		return nil, ErrStoreUnavailable
	}
	return s.store.ListBalances()
}

func (s *Service) ListPlanConfigs(_ context.Context) ([]PlanConfig, error) {
	if s.store == nil {
		return nil, ErrStoreUnavailable
	}
	catalog, err := s.loadPlanCatalog()
	if err != nil {
		return nil, err
	}
	out := make([]PlanConfig, 0, len(SupportedPlans()))
	for _, planID := range SupportedPlans() {
		cfg := catalog[planID]
		out = append(out, cfg)
	}
	return out, nil
}

func (s *Service) SetPlanConfigs(_ context.Context, updates []PlanConfig) ([]PlanConfig, error) {
	if s.store == nil {
		return nil, ErrStoreUnavailable
	}
	catalog, err := s.loadPlanCatalog()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for _, raw := range updates {
		planID := normalizePlanID(raw.PlanID)
		if _, ok := defaultPlanCatalog[planID]; !ok {
			return nil, ErrInvalidPlan
		}
		if raw.PriceCNY < 0 {
			return nil, ErrInvalidAmount
		}
		if raw.RPMLimit < 0 || raw.FiveHourTokenLimit < 0 || raw.MonthlyTokenLimit < 0 {
			return nil, ErrInvalidLimit
		}
		cfg := catalog[planID]
		cfg.PlanID = planID
		cfg.PriceCNY = round2(raw.PriceCNY)
		cfg.RPMLimit = raw.RPMLimit
		cfg.FiveHourTokenLimit = raw.FiveHourTokenLimit
		cfg.MonthlyTokenLimit = raw.MonthlyTokenLimit
		cfg.Unlimited = raw.Unlimited
		if cfg.Unlimited {
			cfg.RPMLimit = 0
			cfg.FiveHourTokenLimit = 0
			cfg.MonthlyTokenLimit = 0
		}
		cfg.UpdatedAt = now
		catalog[planID] = cfg
	}
	ordered := orderedPlanConfigs(catalog)
	if err := s.store.SavePlanConfigs(ordered); err != nil {
		return nil, err
	}
	return ordered, nil
}

func (s *Service) Topup(_ context.Context, userID string, _ TopupRequest) (Balance, Order, error) {
	if s.store == nil {
		return Balance{}, Order{}, ErrStoreUnavailable
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return Balance{}, Order{}, ErrInvalidUserID
	}
	return Balance{}, Order{}, ErrTopupDisabled
}

func (s *Service) Subscribe(ctx context.Context, userID string, req SubscribeRequest) (Subscription, Order, error) {
	if s.store == nil {
		return Subscription{}, Order{}, ErrStoreUnavailable
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return Subscription{}, Order{}, ErrInvalidUserID
	}
	catalog, err := s.loadPlanCatalog()
	if err != nil {
		return Subscription{}, Order{}, err
	}
	plan, ok := catalog[normalizePlanID(req.PlanID)]
	if !ok || plan.PlanID == "free" {
		return Subscription{}, Order{}, ErrInvalidPlan
	}
	channel := strings.ToLower(strings.TrimSpace(req.Channel))
	if channel == "" {
		channel = "stripe"
	}
	if !isSupportedChannel(channel) {
		return Subscription{}, Order{}, ErrInvalidChannel
	}
	if req.Months == 0 {
		req.Months = 1
	}
	if req.Months != 1 {
		return Subscription{}, Order{}, ErrInvalidSubscription
	}

	now := time.Now().UTC()
	current, _ := s.GetMembership(ctx, userID)
	periodStart := now
	if strings.EqualFold(current.Status, "active") && strings.EqualFold(current.PlanID, plan.PlanID) && current.PeriodEnd.After(now) {
		periodStart = current.PeriodEnd
	}
	periodEnd := periodStart.AddDate(0, req.Months, 0)

	sub := Subscription{
		ID:          newID("sub_"),
		UserID:      userID,
		PlanID:      plan.PlanID,
		Status:      "active",
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		CreatedAt:   now,
	}
	order := Order{
		ID:          newID("ord_"),
		UserID:      userID,
		Channel:     channel,
		Type:        "subscription",
		PlanID:      plan.PlanID,
		Amount:      plan.PriceCNY,
		Currency:    settlementCurrency,
		Status:      "paid",
		Description: "membership purchase",
		PeriodStart: sub.PeriodStart,
		PeriodEnd:   sub.PeriodEnd,
		CreatedAt:   now,
	}

	sub, err = s.store.CreateSubscription(sub)
	if err != nil {
		return Subscription{}, Order{}, err
	}
	order, err = s.store.CreateOrder(order)
	if err != nil {
		return Subscription{}, Order{}, err
	}

	if s.logger != nil {
		s.logger.Info("billing subscribe", "user_id", userID, "plan_id", sub.PlanID, "order_id", order.ID)
	}
	return sub, order, nil
}

func (s *Service) SetMembershipAdmin(_ context.Context, userID string, req AdminSetMembershipRequest) (Subscription, error) {
	if s.store == nil {
		return Subscription{}, ErrStoreUnavailable
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return Subscription{}, ErrInvalidUserID
	}
	catalog, err := s.loadPlanCatalog()
	if err != nil {
		return Subscription{}, err
	}
	plan, ok := catalog[normalizePlanID(req.PlanID)]
	if !ok {
		return Subscription{}, ErrInvalidPlan
	}

	now := time.Now().UTC()
	sub := Subscription{
		ID:        newID("sub_"),
		UserID:    userID,
		PlanID:    plan.PlanID,
		CreatedAt: now,
	}
	if plan.PlanID == "free" {
		sub.Status = "free"
		sub.PeriodStart = now
		sub.PeriodEnd = now
	} else {
		if req.PeriodEnd.IsZero() {
			return Subscription{}, ErrInvalidPeriodEnd
		}
		periodEnd := req.PeriodEnd.UTC()
		if !periodEnd.After(now) {
			return Subscription{}, ErrInvalidPeriodEnd
		}
		sub.Status = "active"
		sub.PeriodStart = now
		sub.PeriodEnd = periodEnd
	}

	saved, err := s.store.CreateSubscription(sub)
	if err != nil {
		return Subscription{}, err
	}
	if s.logger != nil {
		s.logger.Info("billing membership set by admin", "user_id", userID, "plan_id", saved.PlanID, "period_end", saved.PeriodEnd.Format(time.RFC3339))
	}
	return saved, nil
}

func (s *Service) GetMembership(_ context.Context, userID string) (Membership, error) {
	if s.store == nil {
		return Membership{}, ErrStoreUnavailable
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return Membership{}, ErrInvalidUserID
	}
	subs, err := s.store.ListSubscriptions()
	if err != nil {
		return Membership{}, err
	}
	now := time.Now().UTC()
	latest, ok := latestSubscriptionByUser(userID, subs)
	if !ok {
		return defaultFreeMembership(userID, now), nil
	}
	return resolveMembershipFromSubscription(latest, now), nil
}

func (s *Service) ListMemberships(_ context.Context) ([]Membership, error) {
	if s.store == nil {
		return nil, ErrStoreUnavailable
	}
	subs, err := s.store.ListSubscriptions()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	latestByUser := make(map[string]Subscription, len(subs))
	for _, sub := range subs {
		userID := strings.TrimSpace(sub.UserID)
		if userID == "" {
			continue
		}
		if existing, ok := latestByUser[userID]; ok && existing.CreatedAt.After(sub.CreatedAt) {
			continue
		}
		latestByUser[userID] = sub
	}
	out := make([]Membership, 0, len(latestByUser))
	for _, sub := range latestByUser {
		out = append(out, resolveMembershipFromSubscription(sub, now))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (s *Service) ResolveUsagePolicy(ctx context.Context, userID string) (UsagePolicy, error) {
	membership, err := s.GetMembership(ctx, userID)
	if err != nil {
		return UsagePolicy{}, err
	}
	catalog, err := s.loadPlanCatalog()
	if err != nil {
		return UsagePolicy{}, err
	}
	effectivePlanID := "free"
	if strings.EqualFold(membership.Status, "active") {
		effectivePlanID = normalizePlanID(membership.PlanID)
	}
	plan, ok := catalog[effectivePlanID]
	if !ok {
		plan = catalog["free"]
	}
	return UsagePolicy{
		PlanID:             plan.PlanID,
		RPMLimit:           plan.RPMLimit,
		FiveHourTokenLimit: plan.FiveHourTokenLimit,
		MonthlyTokenLimit:  plan.MonthlyTokenLimit,
		Unlimited:          plan.Unlimited,
	}, nil
}

func (s *Service) ChargeUsage(_ context.Context, userID string, promptTokens, completionTokens int) (Balance, float64, error) {
	if s.store == nil {
		return Balance{}, 0, ErrStoreUnavailable
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return Balance{}, 0, ErrInvalidUserID
	}
	if promptTokens < 0 || completionTokens < 0 {
		return Balance{}, 0, ErrInvalidUsage
	}
	bal, err := s.store.GetOrCreateBalance(userID, settlementCurrency)
	if err != nil {
		return Balance{}, 0, err
	}
	return bal, 0, nil
}

func (s *Service) ListOrders(ctx context.Context, userID string, limit int) ([]Order, error) {
	page, err := s.ListOrdersAdmin(ctx, OrderQuery{UserID: userID, Limit: limit})
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
		ID:       strings.TrimSpace(query.ID),
		UserID:   strings.TrimSpace(query.UserID),
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

	return OrderPage{Data: orders, Total: total, Limit: limit, Offset: offset}, nil
}

func (s *Service) DeleteOrderAdmin(_ context.Context, orderID string) (Order, error) {
	if s.store == nil {
		return Order{}, ErrStoreUnavailable
	}
	return s.store.DeleteOrder(orderID)
}

func (s *Service) SetBalanceAdmin(_ context.Context, userID string, amount float64) (Balance, error) {
	if s.store == nil {
		return Balance{}, ErrStoreUnavailable
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return Balance{}, ErrInvalidUserID
	}
	if amount < 0 {
		return Balance{}, ErrInvalidAmount
	}
	target := round2(amount)
	current, err := s.store.GetOrCreateBalance(userID, settlementCurrency)
	if err != nil {
		return Balance{}, err
	}
	currentAmount := round2(current.Amount)
	switch {
	case target > currentAmount:
		diff := round2(target - currentAmount)
		return s.store.AddBalance(userID, diff, settlementCurrency)
	case target < currentAmount:
		diff := round2(currentAmount - target)
		return s.store.DeductBalance(userID, diff, settlementCurrency)
	default:
		return current, nil
	}
}

func SupportedPlans() []string {
	return []string{"free", "starter", "pro", "business"}
}

func FreeUsagePolicy() UsagePolicy {
	plan := defaultPlanCatalog["free"]
	return UsagePolicy{
		PlanID:             plan.PlanID,
		RPMLimit:           plan.RPMLimit,
		FiveHourTokenLimit: plan.FiveHourTokenLimit,
		MonthlyTokenLimit:  plan.MonthlyTokenLimit,
		Unlimited:          plan.Unlimited,
	}
}

func defaultFreeMembership(userID string, now time.Time) Membership {
	return Membership{
		UserID:    userID,
		PlanID:    "free",
		Status:    "free",
		UpdatedAt: now,
	}
}

func resolveMembershipFromSubscription(sub Subscription, now time.Time) Membership {
	m := Membership{
		UserID:      strings.TrimSpace(sub.UserID),
		PlanID:      normalizePlanID(sub.PlanID),
		Status:      strings.ToLower(strings.TrimSpace(sub.Status)),
		PeriodStart: sub.PeriodStart,
		PeriodEnd:   sub.PeriodEnd,
		UpdatedAt:   sub.CreatedAt,
	}
	if m.PlanID == "" {
		m.PlanID = "free"
	}
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}
	if m.PlanID == "free" {
		m.Status = "free"
		return m
	}
	if strings.EqualFold(m.Status, "active") && m.PeriodEnd.After(now) {
		m.Status = "active"
		return m
	}
	m.Status = "expired"
	return m
}

func latestSubscriptionByUser(userID string, subs []Subscription) (Subscription, bool) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return Subscription{}, false
	}
	var found Subscription
	ok := false
	for _, sub := range subs {
		if strings.TrimSpace(sub.UserID) != userID {
			continue
		}
		if !ok || sub.CreatedAt.After(found.CreatedAt) {
			found = sub
			ok = true
		}
	}
	return found, ok
}

func lookupPlan(planID string) (PlanConfig, bool) {
	plan, ok := defaultPlanCatalog[normalizePlanID(planID)]
	return plan, ok
}

func normalizePlanID(planID string) string {
	return strings.ToLower(strings.TrimSpace(planID))
}

func isSupportedChannel(ch string) bool {
	switch strings.ToLower(strings.TrimSpace(ch)) {
	case "stripe", "yipay", "manual":
		return true
	default:
		return false
	}
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func (s *Service) loadPlanCatalog() (map[string]PlanConfig, error) {
	if s.store == nil {
		return nil, ErrStoreUnavailable
	}
	items, err := s.store.ListPlanConfigs()
	if err != nil {
		return nil, err
	}
	catalog := make(map[string]PlanConfig, len(defaultPlanCatalog))
	for k, v := range defaultPlanCatalog {
		catalog[k] = v
	}
	changed := len(items) == 0
	for _, item := range items {
		planID := normalizePlanID(item.PlanID)
		if planID == "" {
			continue
		}
		if _, ok := defaultPlanCatalog[planID]; !ok {
			continue
		}
		cfg := catalog[planID]
		cfg.PlanID = planID
		if item.PriceCNY >= 0 {
			cfg.PriceCNY = round2(item.PriceCNY)
		}
		if item.RPMLimit >= 0 {
			cfg.RPMLimit = item.RPMLimit
		}
		if item.FiveHourTokenLimit >= 0 {
			cfg.FiveHourTokenLimit = item.FiveHourTokenLimit
		}
		if item.MonthlyTokenLimit >= 0 {
			cfg.MonthlyTokenLimit = item.MonthlyTokenLimit
		}
		cfg.Unlimited = item.Unlimited
		if cfg.Unlimited {
			cfg.RPMLimit = 0
			cfg.FiveHourTokenLimit = 0
			cfg.MonthlyTokenLimit = 0
		}
		if !item.UpdatedAt.IsZero() {
			cfg.UpdatedAt = item.UpdatedAt
		}
		catalog[planID] = cfg
	}
	for _, planID := range SupportedPlans() {
		if _, ok := catalog[planID]; !ok {
			changed = true
			catalog[planID] = defaultPlanCatalog[planID]
		}
	}
	if changed {
		if err := s.store.SavePlanConfigs(orderedPlanConfigs(catalog)); err != nil {
			return nil, err
		}
	}
	return catalog, nil
}

func orderedPlanConfigs(catalog map[string]PlanConfig) []PlanConfig {
	out := make([]PlanConfig, 0, len(SupportedPlans()))
	for _, planID := range SupportedPlans() {
		cfg, ok := catalog[planID]
		if !ok {
			cfg = defaultPlanCatalog[planID]
		}
		cfg.PlanID = planID
		out = append(out, cfg)
	}
	return out
}
