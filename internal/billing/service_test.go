package billing

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestGetMembershipDefaultFree(t *testing.T) {
	s := newTestService(t)
	m, err := s.GetMembership(context.Background(), "usr_free")
	if err != nil {
		t.Fatalf("get membership failed: %v", err)
	}
	if m.PlanID != "free" || m.Status != "free" {
		t.Fatalf("unexpected free membership: %+v", m)
	}

	policy, err := s.ResolveUsagePolicy(context.Background(), "usr_free")
	if err != nil {
		t.Fatalf("resolve usage policy failed: %v", err)
	}
	if policy.PlanID != "free" || policy.Unlimited {
		t.Fatalf("unexpected free policy: %+v", policy)
	}
	if policy.FiveHourTokenLimit <= 0 || policy.MonthlyTokenLimit <= 0 {
		t.Fatalf("free policy should have finite limits: %+v", policy)
	}
}

func TestSubscribeCreatesMembershipOrderFixedPrice(t *testing.T) {
	s := newTestService(t)

	sub, ord, err := s.Subscribe(context.Background(), "usr_sub", SubscribeRequest{
		PlanID:  "starter",
		Channel: "stripe",
		Months:  1,
	})
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	if sub.PlanID != "starter" || sub.Status != "active" {
		t.Fatalf("unexpected subscription: %+v", sub)
	}
	if !sub.PeriodEnd.After(sub.PeriodStart) {
		t.Fatalf("invalid subscription period: %+v", sub)
	}
	if ord.Type != "subscription" || ord.PlanID != "starter" {
		t.Fatalf("unexpected order: %+v", ord)
	}
	if ord.Currency != "CNY" {
		t.Fatalf("unexpected order currency: %s", ord.Currency)
	}
	if ord.Amount != 29 {
		t.Fatalf("starter plan should use fixed price 29 CNY, got %v", ord.Amount)
	}

	m, err := s.GetMembership(context.Background(), "usr_sub")
	if err != nil {
		t.Fatalf("get membership failed: %v", err)
	}
	if m.PlanID != "starter" || m.Status != "active" {
		t.Fatalf("unexpected membership after subscribe: %+v", m)
	}
}

func TestPlanConfigCanBeUpdatedByAdmin(t *testing.T) {
	s := newTestService(t)
	_, err := s.SetPlanConfigs(context.Background(), []PlanConfig{
		{
			PlanID:             "starter",
			PriceCNY:           66,
			RPMLimit:           222,
			FiveHourTokenLimit: 123456,
			MonthlyTokenLimit:  987654,
			Unlimited:          false,
		},
	})
	if err != nil {
		t.Fatalf("set plan configs failed: %v", err)
	}

	p, err := s.ResolveUsagePolicy(context.Background(), "usr_free_plan")
	if err != nil {
		t.Fatalf("resolve usage policy failed: %v", err)
	}
	if p.PlanID != "free" {
		t.Fatalf("free user plan should remain free, got %+v", p)
	}

	sub, ord, err := s.Subscribe(context.Background(), "usr_cfg", SubscribeRequest{
		PlanID:  "starter",
		Channel: "stripe",
		Months:  1,
	})
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	if sub.PlanID != "starter" {
		t.Fatalf("unexpected plan: %+v", sub)
	}
	if ord.Amount != 66 {
		t.Fatalf("starter price should follow admin config, got %v", ord.Amount)
	}

	mPolicy, err := s.ResolveUsagePolicy(context.Background(), "usr_cfg")
	if err != nil {
		t.Fatalf("resolve usage policy for subscribed user failed: %v", err)
	}
	if mPolicy.RPMLimit != 222 || mPolicy.FiveHourTokenLimit != 123456 || mPolicy.MonthlyTokenLimit != 987654 {
		t.Fatalf("unexpected configured policy: %+v", mPolicy)
	}
}

func TestSubscribeRejectsInvalidRequests(t *testing.T) {
	s := newTestService(t)

	if _, _, err := s.Subscribe(context.Background(), "", SubscribeRequest{PlanID: "starter", Channel: "stripe", Months: 1}); err != ErrInvalidUserID {
		t.Fatalf("expected ErrInvalidUserID, got %v", err)
	}
	if _, _, err := s.Subscribe(context.Background(), "usr_1", SubscribeRequest{PlanID: "", Channel: "stripe", Months: 1}); err != ErrInvalidPlan {
		t.Fatalf("expected ErrInvalidPlan, got %v", err)
	}
	if _, _, err := s.Subscribe(context.Background(), "usr_1", SubscribeRequest{PlanID: "free", Channel: "stripe", Months: 1}); err != ErrInvalidPlan {
		t.Fatalf("expected ErrInvalidPlan for free plan subscribe, got %v", err)
	}
	if _, _, err := s.Subscribe(context.Background(), "usr_1", SubscribeRequest{PlanID: "starter", Channel: "stripe", Months: 2}); err != ErrInvalidSubscription {
		t.Fatalf("expected ErrInvalidSubscription for months!=1, got %v", err)
	}
}

func TestSetMembershipAdmin(t *testing.T) {
	s := newTestService(t)

	end := time.Now().Add(72 * time.Hour)
	sub, err := s.SetMembershipAdmin(context.Background(), "usr_admin_set", AdminSetMembershipRequest{
		PlanID:    "pro",
		PeriodEnd: end,
	})
	if err != nil {
		t.Fatalf("set membership failed: %v", err)
	}
	if sub.PlanID != "pro" || sub.Status != "active" {
		t.Fatalf("unexpected admin subscription: %+v", sub)
	}

	m, err := s.GetMembership(context.Background(), "usr_admin_set")
	if err != nil {
		t.Fatalf("get membership failed: %v", err)
	}
	if m.PlanID != "pro" || m.Status != "active" {
		t.Fatalf("unexpected membership after admin set: %+v", m)
	}

	_, err = s.SetMembershipAdmin(context.Background(), "usr_admin_set", AdminSetMembershipRequest{PlanID: "pro"})
	if err != ErrInvalidPeriodEnd {
		t.Fatalf("expected ErrInvalidPeriodEnd, got %v", err)
	}
}

func TestResolveUsagePolicyBusinessUnlimited(t *testing.T) {
	s := newTestService(t)
	_, err := s.SetMembershipAdmin(context.Background(), "usr_biz", AdminSetMembershipRequest{
		PlanID:    "business",
		PeriodEnd: time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("set business membership failed: %v", err)
	}

	p, err := s.ResolveUsagePolicy(context.Background(), "usr_biz")
	if err != nil {
		t.Fatalf("resolve usage policy failed: %v", err)
	}
	if p.PlanID != "business" || !p.Unlimited {
		t.Fatalf("unexpected business policy: %+v", p)
	}
	if p.FiveHourTokenLimit != 0 || p.MonthlyTokenLimit != 0 {
		t.Fatalf("business should be unlimited, got %+v", p)
	}
}

func TestListOrdersAdminFilterAndPagination(t *testing.T) {
	s := newTestService(t)

	if _, _, err := s.Subscribe(context.Background(), "usr_a", SubscribeRequest{PlanID: "starter", Channel: "stripe", Months: 1}); err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, _, err := s.Subscribe(context.Background(), "usr_a", SubscribeRequest{PlanID: "pro", Channel: "stripe", Months: 1}); err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, _, err := s.Subscribe(context.Background(), "usr_b", SubscribeRequest{PlanID: "starter", Channel: "yipay", Months: 1}); err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	page, err := s.ListOrdersAdmin(context.Background(), OrderQuery{
		UserID: "usr_a",
		Type:   "subscription",
		Limit:  1,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("list orders admin failed: %v", err)
	}
	if page.Total != 2 || len(page.Data) != 1 {
		t.Fatalf("unexpected page data: total=%d count=%d", page.Total, len(page.Data))
	}
	if page.Data[0].Type != "subscription" || page.Data[0].UserID != "usr_a" {
		t.Fatalf("unexpected order: %+v", page.Data[0])
	}
}

func TestTopupDisabled(t *testing.T) {
	s := newTestService(t)
	if _, _, err := s.Topup(context.Background(), "usr_topup", TopupRequest{Channel: "manual", Amount: 1, Currency: "CNY"}); err != ErrTopupDisabled {
		t.Fatalf("expected ErrTopupDisabled, got %v", err)
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	store, err := NewFileStore(filepath.Join(t.TempDir(), "billing.json"))
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	return NewService(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
