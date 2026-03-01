package billing

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestTopupAndBalance(t *testing.T) {
	s := newTestService(t)

	bal, ord, err := s.Topup(context.Background(), "ten_1", TopupRequest{
		Channel:  "manual",
		Amount:   100,
		Currency: "CNY",
	})
	if err != nil {
		t.Fatalf("topup failed: %v", err)
	}
	if bal.Amount != 100 {
		t.Fatalf("unexpected balance amount: %v", bal.Amount)
	}
	if ord.Type != "topup" {
		t.Fatalf("unexpected order type: %s", ord.Type)
	}

	bal2, err := s.GetBalance(context.Background(), "ten_1")
	if err != nil {
		t.Fatalf("get balance failed: %v", err)
	}
	if bal2.Amount != 100 {
		t.Fatalf("unexpected balance amount: %v", bal2.Amount)
	}
}

func TestSubscribeAndListOrders(t *testing.T) {
	s := newTestService(t)

	sub, ord, err := s.Subscribe(context.Background(), "ten_2", SubscribeRequest{
		PlanID:   "pro",
		Channel:  "stripe",
		Amount:   29.9,
		Currency: "USD",
		Months:   1,
	})
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	if sub.PlanID != "pro" {
		t.Fatalf("unexpected plan id: %s", sub.PlanID)
	}
	if ord.Type != "subscription" {
		t.Fatalf("unexpected order type: %s", ord.Type)
	}

	orders, err := s.ListOrders(context.Background(), "ten_2", 10)
	if err != nil {
		t.Fatalf("list orders failed: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("expected 1 order, got %d", len(orders))
	}
}

func TestInvalidRequests(t *testing.T) {
	s := newTestService(t)

	if _, _, err := s.Topup(context.Background(), "ten_1", TopupRequest{Channel: "manual", Amount: 0, Currency: "CNY"}); err != ErrInvalidAmount {
		t.Fatalf("expected ErrInvalidAmount, got %v", err)
	}
	if _, _, err := s.Topup(context.Background(), "ten_1", TopupRequest{Channel: "unknown", Amount: 1, Currency: "CNY"}); err != ErrInvalidChannel {
		t.Fatalf("expected ErrInvalidChannel, got %v", err)
	}
	if _, _, err := s.Subscribe(context.Background(), "ten_1", SubscribeRequest{PlanID: "", Channel: "stripe", Amount: 1, Currency: "USD", Months: 1}); err != ErrInvalidPlan {
		t.Fatalf("expected ErrInvalidPlan, got %v", err)
	}
}

func TestListOrdersAdminFilterAndPagination(t *testing.T) {
	s := newTestService(t)

	if _, _, err := s.Topup(context.Background(), "ten_a", TopupRequest{Channel: "manual", Amount: 9, Currency: "CNY"}); err != nil {
		t.Fatalf("topup failed: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, _, err := s.Subscribe(context.Background(), "ten_a", SubscribeRequest{PlanID: "pro", Channel: "stripe", Amount: 19, Currency: "USD", Months: 1}); err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, _, err := s.Topup(context.Background(), "ten_b", TopupRequest{Channel: "manual", Amount: 6, Currency: "CNY"}); err != nil {
		t.Fatalf("topup failed: %v", err)
	}

	page, err := s.ListOrdersAdmin(context.Background(), OrderQuery{
		TenantID: "ten_a",
		Type:     "topup",
		Limit:    1,
		Offset:   0,
	})
	if err != nil {
		t.Fatalf("list orders admin failed: %v", err)
	}
	if page.Total != 1 || len(page.Data) != 1 {
		t.Fatalf("unexpected page data: total=%d count=%d", page.Total, len(page.Data))
	}
	if page.Data[0].Type != "topup" || page.Data[0].TenantID != "ten_a" {
		t.Fatalf("unexpected order: %+v", page.Data[0])
	}
}

func TestChargeUsage(t *testing.T) {
	s := newTestService(t)

	if _, _, err := s.Topup(context.Background(), "ten_charge", TopupRequest{
		Channel:  "manual",
		Amount:   1,
		Currency: "CNY",
	}); err != nil {
		t.Fatalf("topup failed: %v", err)
	}

	before, err := s.GetBalance(context.Background(), "ten_charge")
	if err != nil {
		t.Fatalf("get balance failed: %v", err)
	}

	after, cost, err := s.ChargeUsage(context.Background(), "ten_charge", 100, 50)
	if err != nil {
		t.Fatalf("charge usage failed: %v", err)
	}
	if cost <= 0 {
		t.Fatalf("expected positive cost, got %v", cost)
	}
	if after.Amount >= before.Amount {
		t.Fatalf("expected balance decrease, before=%v after=%v", before.Amount, after.Amount)
	}
}

func TestChargeUsageInsufficientBalance(t *testing.T) {
	s := newTestService(t)

	if _, _, err := s.ChargeUsage(context.Background(), "ten_no_funds", 10, 10); err != ErrInsufficientBalance {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
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
