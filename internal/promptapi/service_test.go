package promptapi

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
)

func TestUpsertPublishAndBuildChatRequest(t *testing.T) {
	s := newTestService(t)

	def, err := s.Upsert(context.Background(), "ten_1", UpsertRequest{
		Slug:         "email_summary",
		Name:         "Email Summary",
		Model:        "stub/test",
		SystemPrompt: "You are helpful",
		UserTemplate: "Please summarize: {{.content}}",
		Variables:    []string{"content"},
		Publish:      true,
	})
	if err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if def.Status != "published" || def.Version != 1 {
		t.Fatalf("unexpected def status/version: %+v", def)
	}

	_, req, err := s.BuildChatRequest(context.Background(), "ten_1", "email_summary", map[string]any{"content": "hello"})
	if err != nil {
		t.Fatalf("build chat request failed: %v", err)
	}
	if req.Model != "stub/test" {
		t.Fatalf("unexpected model: %s", req.Model)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("unexpected message length: %d", len(req.Messages))
	}
}

func TestBuildChatRequestRequiresPublished(t *testing.T) {
	s := newTestService(t)

	if _, err := s.Upsert(context.Background(), "ten_1", UpsertRequest{
		Slug:         "draft_api",
		Name:         "Draft API",
		Model:        "stub/test",
		UserTemplate: "hello {{.name}}",
		Variables:    []string{"name"},
	}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	_, _, err := s.BuildChatRequest(context.Background(), "ten_1", "draft_api", map[string]any{"name": "opc"})
	if err != ErrNotPublished {
		t.Fatalf("expected ErrNotPublished, got %v", err)
	}
}

func TestListPromptAPIs(t *testing.T) {
	s := newTestService(t)

	_, _ = s.Upsert(context.Background(), "ten_1", UpsertRequest{Slug: "aa", Name: "A", Model: "stub/test", UserTemplate: "{{.x}}", Variables: []string{"x"}, Publish: true})
	_, _ = s.Upsert(context.Background(), "ten_1", UpsertRequest{Slug: "bb", Name: "B", Model: "stub/test", UserTemplate: "{{.x}}", Variables: []string{"x"}})
	_, _ = s.Upsert(context.Background(), "ten_2", UpsertRequest{Slug: "cc", Name: "C", Model: "stub/test", UserTemplate: "{{.x}}", Variables: []string{"x"}, Publish: true})

	page, err := s.List(context.Background(), ListQuery{TenantID: "ten_1", Status: "published", Limit: 10})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if page.Total != 1 || len(page.Data) != 1 {
		t.Fatalf("unexpected list page: total=%d count=%d", page.Total, len(page.Data))
	}
	if page.Data[0].Slug != "aa" {
		t.Fatalf("unexpected slug: %s", page.Data[0].Slug)
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	store, err := NewFileStore(filepath.Join(t.TempDir(), "prompt_apis.json"))
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	return NewService(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
