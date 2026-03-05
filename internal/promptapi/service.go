package promptapi

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/derekwin/opcAPI/internal/provider"
)

var slugPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{2,64}$`)
var simpleTemplateVarPattern = regexp.MustCompile(`{{\s*\.([a-zA-Z0-9_]+)\s*}}`)

const (
	DefaultAPIChatCompletions = "chat_completions"
	DefaultAPIModels          = "models"
)

type defaultAPIMeta struct {
	APIKey        string
	APIPath       string
	Name          string
	SupportsModel bool
}

var defaultAPIRegistry = []defaultAPIMeta{
	{
		APIKey:        DefaultAPIChatCompletions,
		APIPath:       "/v1/chat/completions",
		Name:          "Chat Completions",
		SupportsModel: true,
	},
	{
		APIKey:        DefaultAPIModels,
		APIPath:       "/v1/models",
		Name:          "Models",
		SupportsModel: false,
	},
}

type Service struct {
	store  *FileStore
	logger *slog.Logger
}

func NewService(store *FileStore, logger *slog.Logger) *Service {
	return &Service{store: store, logger: logger}
}

func (s *Service) Upsert(ctx context.Context, req UpsertRequest) (Definition, error) {
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

	existing, found, err := s.store.Get(slug)
	if err != nil {
		return Definition{}, err
	}

	if !found {
		def := Definition{
			ID:            newID("papi_"),
			Slug:          slug,
			Name:          name,
			Description:   strings.TrimSpace(req.Description),
			Model:         model,
			InputSchema:   strings.TrimSpace(req.InputSchema),
			OutputSchema:  strings.TrimSpace(req.OutputSchema),
			SystemPrompt:  strings.TrimSpace(req.SystemPrompt),
			UserTemplate:  userTemplate,
			Variables:     variables,
			ExampleInput:  strings.TrimSpace(req.ExampleInput),
			ExampleOutput: strings.TrimSpace(req.ExampleOutput),
			Status:        "draft",
			Version:       1,
			CreatedAt:     now,
			UpdatedAt:     now,
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
			s.logger.Info("prompt api created", "slug", slug, "status", def.Status, "version", def.Version)
		}
		return def, nil
	}

	changed := existing.Name != name ||
		existing.Description != strings.TrimSpace(req.Description) ||
		existing.Model != model ||
		existing.InputSchema != strings.TrimSpace(req.InputSchema) ||
		existing.OutputSchema != strings.TrimSpace(req.OutputSchema) ||
		existing.SystemPrompt != strings.TrimSpace(req.SystemPrompt) ||
		existing.UserTemplate != userTemplate ||
		!slices.Equal(existing.Variables, variables) ||
		existing.ExampleInput != strings.TrimSpace(req.ExampleInput) ||
		existing.ExampleOutput != strings.TrimSpace(req.ExampleOutput)

	existing.Name = name
	existing.Description = strings.TrimSpace(req.Description)
	existing.Model = model
	existing.InputSchema = strings.TrimSpace(req.InputSchema)
	existing.OutputSchema = strings.TrimSpace(req.OutputSchema)
	existing.SystemPrompt = strings.TrimSpace(req.SystemPrompt)
	existing.UserTemplate = userTemplate
	existing.Variables = variables
	existing.ExampleInput = strings.TrimSpace(req.ExampleInput)
	existing.ExampleOutput = strings.TrimSpace(req.ExampleOutput)
	existing.UpdatedAt = now
	if changed {
		existing.Version++
	}
	if req.Publish {
		existing.Status = "published"
		publishedAt := now
		existing.PublishedAt = &publishedAt
	} else {
		existing.Status = "draft"
		existing.PublishedAt = nil
	}

	updated, err := s.store.Upsert(existing)
	if err != nil {
		return Definition{}, err
	}
	if s.logger != nil {
		s.logger.Info("prompt api updated", "slug", slug, "status", updated.Status, "version", updated.Version)
	}
	return updated, nil
}

func (s *Service) List(ctx context.Context, q ListQuery) (ListPage, error) {
	_ = ctx
	if s.store == nil {
		return ListPage{}, ErrStoreUnavailable
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
		Status: q.Status,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return ListPage{}, err
	}
	return ListPage{Data: items, Total: total, Limit: limit, Offset: offset}, nil
}

func (s *Service) ListDefaultAPIs(ctx context.Context) ([]DefaultAPIConfig, error) {
	_ = ctx
	if s.store == nil {
		return nil, ErrStoreUnavailable
	}
	stored, err := s.store.ListDefaultAPIs()
	if err != nil {
		return nil, err
	}

	byKey := make(map[string]DefaultAPIConfig, len(stored))
	for _, item := range stored {
		key := strings.TrimSpace(item.APIKey)
		if key == "" {
			continue
		}
		byKey[key] = item
	}

	now := time.Now().UTC()
	out := make([]DefaultAPIConfig, 0, len(defaultAPIRegistry))
	for _, meta := range defaultAPIRegistry {
		item, ok := byKey[meta.APIKey]
		if !ok {
			item = DefaultAPIConfig{
				APIKey:    meta.APIKey,
				APIPath:   meta.APIPath,
				Name:      meta.Name,
				Enabled:   true,
				UpdatedAt: now,
			}
		} else {
			item.APIKey = meta.APIKey
			item.APIPath = meta.APIPath
			item.Name = meta.Name
			if item.UpdatedAt.IsZero() {
				item.UpdatedAt = now
			}
			if !meta.SupportsModel {
				item.DefaultModel = ""
				item.AllowedModels = nil
				item.ModelIDMappings = nil
			}
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].APIPath < out[j].APIPath
	})
	return out, nil
}

func (s *Service) UpsertDefaultAPI(ctx context.Context, req UpsertDefaultAPIRequest) (DefaultAPIConfig, error) {
	_ = ctx
	if s.store == nil {
		return DefaultAPIConfig{}, ErrStoreUnavailable
	}

	meta, ok := defaultAPIMetaByKey(req.APIKey)
	if !ok {
		return DefaultAPIConfig{}, fmt.Errorf("%w: unknown api key", ErrInvalidDefaultAPI)
	}

	allowed := normalizeTextSet(req.AllowedModels)
	defaultModel := strings.TrimSpace(req.DefaultModel)
	mappings, err := normalizeDefaultAPIMappings(req.ModelIDMappings)
	if err != nil {
		return DefaultAPIConfig{}, err
	}

	if !meta.SupportsModel {
		defaultModel = ""
		allowed = nil
		mappings = nil
	}
	if meta.SupportsModel {
		if defaultModel != "" && len(allowed) > 0 && !containsString(allowed, defaultModel) {
			return DefaultAPIConfig{}, fmt.Errorf("%w: default model must be in allowed models", ErrInvalidDefaultAPI)
		}
		if len(mappings) > 0 && len(allowed) > 0 {
			for _, item := range mappings {
				if !containsString(allowed, item.Model) {
					return DefaultAPIConfig{}, fmt.Errorf("%w: mapped model must be in allowed models", ErrInvalidDefaultAPI)
				}
			}
		}
	}

	cfg := DefaultAPIConfig{
		APIKey:          meta.APIKey,
		APIPath:         meta.APIPath,
		Name:            meta.Name,
		Enabled:         req.Enabled,
		DefaultModel:    defaultModel,
		AllowedModels:   allowed,
		ModelIDMappings: mappings,
		UpdatedAt:       time.Now().UTC(),
	}
	saved, err := s.store.UpsertDefaultAPI(cfg)
	if err != nil {
		return DefaultAPIConfig{}, err
	}
	if s.logger != nil {
		s.logger.Info("default api config updated", "api_key", saved.APIKey, "enabled", saved.Enabled)
	}
	return saved, nil
}

func (s *Service) ResolveModelForAPI(ctx context.Context, apiKey, model string) (string, bool, error) {
	_ = ctx
	if s.store == nil {
		return "", false, ErrStoreUnavailable
	}
	meta, ok := defaultAPIMetaByKey(apiKey)
	if !ok {
		return "", false, fmt.Errorf("%w: unknown api key", ErrInvalidDefaultAPI)
	}
	if !meta.SupportsModel {
		return "", false, nil
	}

	cfg, err := s.defaultAPIConfigByKey(apiKey)
	if err != nil {
		return "", false, err
	}
	if !cfg.Enabled {
		return "", false, ErrModelNotAllowed
	}

	requested := strings.TrimSpace(model)
	allowed := cfg.AllowedModels
	mapping := mappingByModelID(cfg.ModelIDMappings)

	if requested == "" {
		if strings.TrimSpace(cfg.DefaultModel) == "" {
			return "", false, nil
		}
		if len(allowed) > 0 && !containsString(allowed, cfg.DefaultModel) {
			return "", false, fmt.Errorf("%w: default model not allowed", ErrInvalidDefaultAPI)
		}
		return cfg.DefaultModel, true, nil
	}

	if len(mapping) > 0 {
		target, ok := mapping[requested]
		if !ok {
			return "", false, ErrModelNotAllowed
		}
		if len(allowed) > 0 && !containsString(allowed, target) {
			return "", false, ErrModelNotAllowed
		}
		return target, false, nil
	}

	if len(allowed) > 0 && !containsString(allowed, requested) {
		return "", false, ErrModelNotAllowed
	}
	return requested, false, nil
}

func (s *Service) ListModelIDsForAPI(ctx context.Context, apiKey string) ([]string, error) {
	_ = ctx
	if s.store == nil {
		return nil, ErrStoreUnavailable
	}
	meta, ok := defaultAPIMetaByKey(apiKey)
	if !ok {
		return nil, fmt.Errorf("%w: unknown api key", ErrInvalidDefaultAPI)
	}
	if !meta.SupportsModel {
		return nil, nil
	}

	cfg, err := s.defaultAPIConfigByKey(apiKey)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, nil
	}
	if len(cfg.ModelIDMappings) > 0 {
		out := make([]string, 0, len(cfg.ModelIDMappings))
		for _, item := range cfg.ModelIDMappings {
			if strings.TrimSpace(item.ModelID) == "" {
				continue
			}
			out = append(out, item.ModelID)
		}
		sort.Strings(out)
		return out, nil
	}
	if len(cfg.AllowedModels) > 0 {
		out := make([]string, len(cfg.AllowedModels))
		copy(out, cfg.AllowedModels)
		sort.Strings(out)
		return out, nil
	}
	return nil, nil
}

func (s *Service) Delete(ctx context.Context, slug string) error {
	_ = ctx
	if s.store == nil {
		return ErrStoreUnavailable
	}
	slug = strings.TrimSpace(slug)
	if !slugPattern.MatchString(slug) {
		return ErrInvalidSlug
	}
	if err := s.store.Delete(slug); err != nil {
		return err
	}
	if s.logger != nil {
		s.logger.Info("prompt api deleted", "slug", slug)
	}
	return nil
}

func (s *Service) BuildChatRequest(ctx context.Context, slug string, input map[string]any) (Definition, provider.ChatRequest, error) {
	_ = ctx
	if s.store == nil {
		return Definition{}, provider.ChatRequest{}, ErrStoreUnavailable
	}
	if strings.TrimSpace(slug) == "" || input == nil {
		return Definition{}, provider.ChatRequest{}, ErrInvalidInput
	}

	def, found, err := s.store.Get(strings.TrimSpace(slug))
	if err != nil {
		return Definition{}, provider.ChatRequest{}, err
	}
	if !found {
		return Definition{}, provider.ChatRequest{}, ErrNotFound
	}
	if def.Status != "published" {
		return Definition{}, provider.ChatRequest{}, ErrNotPublished
	}
	req, _, err := buildChatRequestFromDefinition(def, input)
	if err != nil {
		return Definition{}, provider.ChatRequest{}, err
	}
	return def, req, nil
}

func (s *Service) BuildPreviewChatRequest(req UpsertRequest, input map[string]any) (Definition, provider.ChatRequest, string, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return Definition{}, provider.ChatRequest{}, "", ErrInvalidModel
	}
	userTemplate := strings.TrimSpace(req.UserTemplate)
	if userTemplate == "" {
		return Definition{}, provider.ChatRequest{}, "", ErrInvalidTemplate
	}
	if input == nil {
		return Definition{}, provider.ChatRequest{}, "", ErrInvalidInput
	}
	if _, err := template.New("prompt").Option("missingkey=error").Parse(userTemplate); err != nil {
		return Definition{}, provider.ChatRequest{}, "", ErrInvalidTemplate
	}

	def := Definition{
		Slug:          strings.TrimSpace(req.Slug),
		Name:          strings.TrimSpace(req.Name),
		Description:   strings.TrimSpace(req.Description),
		Model:         model,
		InputSchema:   strings.TrimSpace(req.InputSchema),
		OutputSchema:  strings.TrimSpace(req.OutputSchema),
		SystemPrompt:  strings.TrimSpace(req.SystemPrompt),
		UserTemplate:  userTemplate,
		Variables:     normalizeVariables(req.Variables),
		ExampleInput:  strings.TrimSpace(req.ExampleInput),
		ExampleOutput: strings.TrimSpace(req.ExampleOutput),
		Status:        "preview",
	}
	chatReq, renderedUserPrompt, err := buildChatRequestFromDefinition(def, input)
	if err != nil {
		return Definition{}, provider.ChatRequest{}, "", err
	}
	return def, chatReq, renderedUserPrompt, nil
}

func buildChatRequestFromDefinition(def Definition, input map[string]any) (provider.ChatRequest, string, error) {
	for _, key := range def.Variables {
		if _, ok := input[key]; !ok {
			return provider.ChatRequest{}, "", fmt.Errorf("%w: missing variable %s", ErrInvalidInput, key)
		}
	}

	tpl, err := template.New("prompt").Option("missingkey=error").Parse(def.UserTemplate)
	if err != nil {
		return provider.ChatRequest{}, "", ErrInvalidTemplate
	}
	renderInput := make(map[string]any, len(input)+4)
	for k, v := range input {
		renderInput[k] = v
	}
	for _, key := range extractSimpleTemplateVars(def.UserTemplate) {
		if _, ok := renderInput[key]; ok {
			continue
		}
		// Keep undeclared placeholders usable in prompts (e.g. {{.output}} -> "output")
		// without forcing callers to pass extra runtime input.
		renderInput[key] = key
	}
	var b strings.Builder
	if err := tpl.Execute(&b, renderInput); err != nil {
		return provider.ChatRequest{}, "", fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	renderedUserPrompt := b.String()

	systemPrompt := composeSystemPrompt(def.SystemPrompt, def.OutputSchema)
	messages := make([]provider.Message, 0, 2)
	if strings.TrimSpace(systemPrompt) != "" {
		messages = append(messages, provider.Message{Role: "system", Content: systemPrompt})
	}
	messages = append(messages, provider.Message{Role: "user", Content: renderedUserPrompt})

	return provider.ChatRequest{Model: def.Model, Messages: messages}, renderedUserPrompt, nil
}

func composeSystemPrompt(systemPrompt, outputSchema string) string {
	base := strings.TrimSpace(systemPrompt)
	outputSchema = strings.TrimSpace(outputSchema)
	if outputSchema == "" {
		return base
	}
	if base == "" {
		return "Output format requirements:\n" + outputSchema
	}
	return base + "\n\nOutput format requirements:\n" + outputSchema
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

func extractSimpleTemplateVars(raw string) []string {
	matches := simpleTemplateVarPattern.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		key := strings.TrimSpace(m[1])
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func defaultAPIMetaByKey(key string) (defaultAPIMeta, bool) {
	target := strings.TrimSpace(key)
	if target == "" {
		return defaultAPIMeta{}, false
	}
	for _, item := range defaultAPIRegistry {
		if item.APIKey == target {
			return item, true
		}
	}
	return defaultAPIMeta{}, false
}

func normalizeTextSet(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		v := strings.TrimSpace(item)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeDefaultAPIMappings(items []DefaultAPIModelMapping) ([]DefaultAPIModelMapping, error) {
	if len(items) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]DefaultAPIModelMapping, 0, len(items))
	for _, item := range items {
		modelID := strings.TrimSpace(item.ModelID)
		model := strings.TrimSpace(item.Model)
		if modelID == "" || model == "" {
			return nil, fmt.Errorf("%w: model id mapping requires model_id and model", ErrInvalidDefaultAPI)
		}
		if _, ok := seen[modelID]; ok {
			return nil, fmt.Errorf("%w: duplicated model id mapping", ErrInvalidDefaultAPI)
		}
		seen[modelID] = struct{}{}
		out = append(out, DefaultAPIModelMapping{ModelID: modelID, Model: model})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ModelID < out[j].ModelID
	})
	return out, nil
}

func mappingByModelID(items []DefaultAPIModelMapping) map[string]string {
	out := make(map[string]string, len(items))
	for _, item := range items {
		modelID := strings.TrimSpace(item.ModelID)
		model := strings.TrimSpace(item.Model)
		if modelID == "" || model == "" {
			continue
		}
		out[modelID] = model
	}
	return out
}

func containsString(items []string, target string) bool {
	if len(items) == 0 {
		return false
	}
	target = strings.TrimSpace(target)
	for _, item := range items {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}

func (s *Service) defaultAPIConfigByKey(apiKey string) (DefaultAPIConfig, error) {
	configs, err := s.ListDefaultAPIs(context.Background())
	if err != nil {
		return DefaultAPIConfig{}, err
	}
	key := strings.TrimSpace(apiKey)
	for _, item := range configs {
		if item.APIKey == key {
			return item, nil
		}
	}
	return DefaultAPIConfig{}, fmt.Errorf("%w: unknown api key", ErrInvalidDefaultAPI)
}
