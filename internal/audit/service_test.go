package audit

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
)

func TestRecordAndList(t *testing.T) {
	s := newTestService(t)

	if _, err := s.Record(context.Background(), RecordInput{
		Resource:       "api_key",
		Action:         "upsert",
		ActorUserID:    "usr_1",
		ActorTenantID:  "ten_1",
		TargetID:       "abc",
		TargetTenantID: "ten_2",
		Metadata:       map[string]string{"enabled": "true"},
	}); err != nil {
		t.Fatalf("record failed: %v", err)
	}
	if _, err := s.Record(context.Background(), RecordInput{
		Resource:       "api_key",
		Action:         "delete",
		ActorUserID:    "usr_1",
		ActorTenantID:  "ten_1",
		TargetID:       "abc",
		TargetTenantID: "ten_2",
	}); err != nil {
		t.Fatalf("record failed: %v", err)
	}

	page, err := s.List(context.Background(), Query{Resource: "api_key", Action: "delete", Limit: 10})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if page.Total != 1 || len(page.Data) != 1 {
		t.Fatalf("unexpected page: total=%d count=%d", page.Total, len(page.Data))
	}
	if page.Data[0].Action != "delete" {
		t.Fatalf("unexpected action: %s", page.Data[0].Action)
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	store, err := NewFileStore(filepath.Join(t.TempDir(), "audit.json"))
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	return NewService(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
