package promptapi

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/derekwin/opcAPI/internal/provider"
)

var slugPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{2,64}$`)

type Service struct {
	store  *FileStore
	logger *slog.Logger
}

func NewService(store *FileStore, logger *slog.Logger) *Service {
	return &Service{store: store, logger: logger}
}

func (s *Service) Upsert(ctx context.Context, tenantID string, req UpsertRequest) (Definition, error) {
	_ = ctx
	if s.store == nil {
		return Definition{}, ErrStoreUnavailable
	}

	slug := strings.TrimSpace(req.Slug)
	if !slugPattern.MatchString(slug) {
		return Definition{}, ErrInvalidSlug
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return Definition{}, ErrInvalidName
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return Definition{}, ErrInvalidModel
	}
	userTemplate := strings.TrimSpace(req.UserTemplate)
	if userTemplate == "" {
		return Definition{}, ErrInvalidTemplate
	}
	if _, err := template.New("prompt").Option("missingkey=error").Parse(userTemplate); err != nil {
		return Definition{}, ErrInvalidTemplate
	}

	variables := normalizeVariables(req.Variables)
	now := time.Now().UTC()

	existing, found, err := s.store.Get(tenantID, slug)
	if err != nil {
		return Definition{}, err
	}

	if !found {
		def := Definition{
			ID:           newID("papi_"),
			TenantID:     tenantID,
			Slug:         slug,
			Name:         name,
			Description:  strings.TrimSpace(req.Description),
			Model:        model,
			SystemPrompt: strings.TrimSpace(req.SystemPrompt),
			UserTemplate: userTemplate,
			Variables:    variables,
			Status:       "draft",
			Version:      1,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if req.Publish {
			def.Status = "published"
			publishedAt := now
			def.PublishedAt = &publishedAt
		}
		def, err = s.store.Upsert(def)
		if err != nil {
			return Definition{}, err
		}
		if s.logger != nil {
			s.logger.Info("prompt api created", "tenant_id", tenantID, "slug", slug, "status", def.Status, "version", def.Version)
		}
		return def, nil
	}

	changed := existing.Name != name ||
		existing.Description != strings.TrimSpace(req.Description) ||
		existing.Model != model ||
		existing.SystemPrompt != strings.TrimSpace(req.SystemPrompt) ||
		existing.UserTemplate != userTemplate ||
		!slices.Equal(existing.Variables, variables)

	existing.Name = name
	existing.Description = strings.TrimSpace(req.Description)
	existing.Model = model
	existing.SystemPrompt = strings.TrimSpace(req.SystemPrompt)
	existing.UserTemplate = userTemplate
	existing.Variables = variables
	existing.UpdatedAt = now
	if changed {
		existing.Version++
	}
	if req.Publish {
		existing.Status = "published"
		publishedAt := now
		existing.PublishedAt = &publishedAt
	} else if strings.TrimSpace(existing.Status) == "" {
		existing.Status = "draft"
	}

	updated, err := s.store.Upsert(existing)
	if err != nil {
		return Definition{}, err
	}
	if s.logger != nil {
		s.logger.Info("prompt api updated", "tenant_id", tenantID, "slug", slug, "status", updated.Status, "version", updated.Version)
	}
	return updated, nil
}

func (s *Service) List(ctx context.Context, q ListQuery) (ListPage, error) {
	_ = ctx
	if s.store == nil {
		return ListPage{}, ErrStoreUnavailable
	}
	if strings.TrimSpace(q.TenantID) == "" {
		return ListPage{}, ErrInvalidInput
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}

	items, total, err := s.store.Query(ListQuery{
		TenantID: q.TenantID,
		Status:   q.Status,
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		return ListPage{}, err
	}
	return ListPage{Data: items, Total: total, Limit: limit, Offset: offset}, nil
}

func (s *Service) BuildChatRequest(ctx context.Context, tenantID, slug string, input map[string]any) (Definition, provider.ChatRequest, error) {
	_ = ctx
	if s.store == nil {
		return Definition{}, provider.ChatRequest{}, ErrStoreUnavailable
	}
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(slug) == "" {
		return Definition{}, provider.ChatRequest{}, ErrInvalidInput
	}
	if input == nil {
		return Definition{}, provider.ChatRequest{}, ErrInvalidInput
	}

	def, found, err := s.store.Get(tenantID, strings.TrimSpace(slug))
	if err != nil {
		return Definition{}, provider.ChatRequest{}, err
	}
	if !found {
		return Definition{}, provider.ChatRequest{}, ErrNotFound
	}
	if def.Status != "published" {
		return Definition{}, provider.ChatRequest{}, ErrNotPublished
	}

	for _, key := range def.Variables {
		if _, ok := input[key]; !ok {
			return Definition{}, provider.ChatRequest{}, fmt.Errorf("%w: missing variable %s", ErrInvalidInput, key)
		}
	}

	tpl, err := template.New("prompt").Option("missingkey=error").Parse(def.UserTemplate)
	if err != nil {
		return Definition{}, provider.ChatRequest{}, ErrInvalidTemplate
	}
	var b strings.Builder
	if err := tpl.Execute(&b, input); err != nil {
		return Definition{}, provider.ChatRequest{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	messages := make([]provider.Message, 0, 2)
	if strings.TrimSpace(def.SystemPrompt) != "" {
		messages = append(messages, provider.Message{Role: "system", Content: def.SystemPrompt})
	}
	messages = append(messages, provider.Message{Role: "user", Content: b.String()})

	return def, provider.ChatRequest{Model: def.Model, Messages: messages}, nil
}

func normalizeVariables(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		n := strings.TrimSpace(v)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}
