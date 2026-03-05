package promptapi

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"
)

func TestUpsertPublishAndBuildChatRequest(t *testing.T) {
	s := newTestService(t)

	def, err := s.Upsert(context.Background(), UpsertRequest{
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

	_, req, err := s.BuildChatRequest(context.Background(), "email_summary", map[string]any{"content": "hello"})
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

	if _, err := s.Upsert(context.Background(), UpsertRequest{
		Slug:         "draft_api",
		Name:         "Draft API",
		Model:        "stub/test",
		UserTemplate: "hello {{.name}}",
		Variables:    []string{"name"},
	}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	_, _, err := s.BuildChatRequest(context.Background(), "draft_api", map[string]any{"name": "opc"})
	if err != ErrNotPublished {
		t.Fatalf("expected ErrNotPublished, got %v", err)
	}
}

func TestListPromptAPIs(t *testing.T) {
	s := newTestService(t)

	_, _ = s.Upsert(context.Background(), UpsertRequest{Slug: "aa", Name: "A", Model: "stub/test", UserTemplate: "{{.x}}", Variables: []string{"x"}, Publish: true})
	_, _ = s.Upsert(context.Background(), UpsertRequest{Slug: "bb", Name: "B", Model: "stub/test", UserTemplate: "{{.x}}", Variables: []string{"x"}})

	page, err := s.List(context.Background(), ListQuery{Status: "published", Limit: 10})
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

func TestBuildPreviewChatRequest(t *testing.T) {
	s := newTestService(t)

	def, req, rendered, err := s.BuildPreviewChatRequest(UpsertRequest{
		Slug:         "preview_api",
		Name:         "Preview API",
		Model:        "stub/test",
		OutputSchema: `{"summary":"string"}`,
		SystemPrompt: "You are helpful",
		UserTemplate: "topic={{.topic}}",
		Variables:    []string{"topic"},
	}, map[string]any{"topic": "routing"})
	if err != nil {
		t.Fatalf("preview build failed: %v", err)
	}
	if def.Status != "preview" {
		t.Fatalf("unexpected preview status: %s", def.Status)
	}
	if req.Model != "stub/test" {
		t.Fatalf("unexpected preview model: %s", req.Model)
	}
	if rendered != "topic=routing" {
		t.Fatalf("unexpected rendered user prompt: %s", rendered)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("unexpected message size: %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
		t.Fatalf("unexpected message roles: %+v", req.Messages)
	}
	if req.Messages[0].Content == "You are helpful" {
		t.Fatalf("output schema should be appended to system prompt")
	}
}

func TestBuildPreviewChatRequestSupportsUndeclaredTemplatePlaceholder(t *testing.T) {
	s := newTestService(t)

	_, req, rendered, err := s.BuildPreviewChatRequest(UpsertRequest{
		Slug:         "translate_preview",
		Name:         "Translate Preview",
		Model:        "stub/test",
		UserTemplate: "src={{.input}}, output_key={{.output}}",
		Variables:    []string{"input"},
	}, map[string]any{"input": "hello"})
	if err != nil {
		t.Fatalf("preview build failed: %v", err)
	}
	if rendered != "src=hello, output_key=output" {
		t.Fatalf("unexpected rendered prompt: %s", rendered)
	}
	if len(req.Messages) != 1 || req.Messages[0].Content != rendered {
		t.Fatalf("unexpected preview messages: %+v", req.Messages)
	}
}

func TestDefaultAPIConfigAndModelResolution(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()

	items, err := s.ListDefaultAPIs(ctx)
	if err != nil {
		t.Fatalf("list default apis failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("expected built-in default apis")
	}

	_, err = s.UpsertDefaultAPI(ctx, UpsertDefaultAPIRequest{
		APIKey:        DefaultAPIChatCompletions,
		Enabled:       true,
		DefaultModel:  "route-default",
		AllowedModels: []string{"route-default", "qwen-plus"},
		ModelIDMappings: []DefaultAPIModelMapping{
			{ModelID: "gpt-4o", Model: "route-default"},
			{ModelID: "qwen-plus", Model: "qwen-plus"},
		},
	})
	if err != nil {
		t.Fatalf("upsert default api failed: %v", err)
	}

	resolvedDefault, usedDefault, err := s.ResolveModelForAPI(ctx, DefaultAPIChatCompletions, "")
	if err != nil {
		t.Fatalf("resolve default model failed: %v", err)
	}
	if !usedDefault || resolvedDefault != "route-default" {
		t.Fatalf("unexpected default resolution: model=%s used_default=%v", resolvedDefault, usedDefault)
	}

	resolvedMapped, usedDefault, err := s.ResolveModelForAPI(ctx, DefaultAPIChatCompletions, "gpt-4o")
	if err != nil {
		t.Fatalf("resolve mapped model failed: %v", err)
	}
	if usedDefault || resolvedMapped != "route-default" {
		t.Fatalf("unexpected mapped resolution: model=%s used_default=%v", resolvedMapped, usedDefault)
	}

	if _, _, err := s.ResolveModelForAPI(ctx, DefaultAPIChatCompletions, "not-allowed"); err != ErrModelNotAllowed {
		t.Fatalf("expected ErrModelNotAllowed, got %v", err)
	}

	modelIDs, err := s.ListModelIDsForAPI(ctx, DefaultAPIChatCompletions)
	if err != nil {
		t.Fatalf("list model ids failed: %v", err)
	}
	if !slices.Equal(modelIDs, []string{"gpt-4o", "qwen-plus"}) {
		t.Fatalf("unexpected model ids: %+v", modelIDs)
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
