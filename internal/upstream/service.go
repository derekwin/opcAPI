package upstream

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type Service struct {
	store   *FileStore
	logger  *slog.Logger
	statsMu sync.Mutex
	stats   map[string]RuntimeStat
}

type candidateWithMeta struct {
	target   ResolvedTarget
	weight   int
	priority int
}

func NewService(store *FileStore, logger *slog.Logger) *Service {
	return &Service{
		store:  store,
		logger: logger,
		stats:  make(map[string]RuntimeStat),
	}
}

func (s *Service) ListUpstreams() ([]Upstream, error) {
	if s == nil || s.store == nil {
		return nil, ErrStoreUnavailable
	}
	return s.store.ListUpstreams()
}

func (s *Service) UpsertUpstream(req UpsertUpstreamRequest) (Upstream, error) {
	if s == nil || s.store == nil {
		return Upstream{}, ErrStoreUnavailable
	}
	up, err := normalizeUpstream(req)
	if err != nil {
		return Upstream{}, err
	}
	if up.ID != "" {
		if existing, ok := s.findUpstreamByID(up.ID); ok {
			if strings.TrimSpace(up.APIKey) == "" {
				up.APIKey = existing.APIKey
			}
			up.LastHealthStatus = existing.LastHealthStatus
			up.LastHealthLatency = existing.LastHealthLatency
			up.LastHealthError = existing.LastHealthError
			up.LastHealthHTTPCode = existing.LastHealthHTTPCode
			up.LastHealthEndpoint = existing.LastHealthEndpoint
			up.LastHealthResponse = existing.LastHealthResponse
			up.LastHealthCheckedAt = existing.LastHealthCheckedAt
		}
	}
	saved, err := s.store.UpsertUpstream(up)
	if err != nil {
		return Upstream{}, err
	}
	if s.logger != nil {
		s.logger.Info("upstream upserted", "upstream_id", saved.ID, "type", saved.Type, "enabled", saved.Enabled)
	}
	return saved, nil
}

func (s *Service) findUpstreamByID(id string) (Upstream, bool) {
	items, err := s.store.ListUpstreams()
	if err != nil {
		return Upstream{}, false
	}
	id = strings.TrimSpace(id)
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return Upstream{}, false
}

func (s *Service) DeleteUpstream(upstreamID string) error {
	if s == nil || s.store == nil {
		return ErrStoreUnavailable
	}
	if err := s.store.DeleteUpstream(strings.TrimSpace(upstreamID)); err != nil {
		return err
	}
	if s.logger != nil {
		s.logger.Info("upstream deleted", "upstream_id", upstreamID)
	}
	return nil
}

func (s *Service) ListRoutes() ([]ModelRoute, error) {
	if s == nil || s.store == nil {
		return nil, ErrStoreUnavailable
	}
	return s.store.ListRoutes()
}

// DefaultRouteAlias returns the admin-configured default alias for end-user traffic.
// Priority:
// 1) enabled route alias equals "default" (case-insensitive)
// 2) first enabled route in deterministic order
func (s *Service) DefaultRouteAlias() (string, bool, error) {
	if s == nil || s.store == nil {
		return "", false, ErrStoreUnavailable
	}

	routes, err := s.store.ListRoutes()
	if err != nil {
		return "", false, err
	}

	firstEnabled := ""
	for _, route := range routes {
		if !route.Enabled {
			continue
		}
		alias := strings.TrimSpace(route.Alias)
		if alias == "" {
			continue
		}
		if firstEnabled == "" {
			firstEnabled = alias
		}
		if strings.EqualFold(alias, "default") {
			return alias, true, nil
		}
	}
	if firstEnabled != "" {
		return firstEnabled, true, nil
	}
	return "", false, nil
}

func (s *Service) UpsertRoute(req UpsertRouteRequest) (ModelRoute, error) {
	if s == nil || s.store == nil {
		return ModelRoute{}, ErrStoreUnavailable
	}
	route, err := normalizeRoute(req)
	if err != nil {
		return ModelRoute{}, err
	}
	saved, err := s.store.UpsertRoute(route)
	if err != nil {
		return ModelRoute{}, err
	}
	if s.logger != nil {
		s.logger.Info("model route upserted", "alias", saved.Alias, "strategy", saved.Strategy, "targets", len(saved.Targets), "enabled", saved.Enabled)
	}
	return saved, nil
}

func (s *Service) DeleteRoute(alias string) error {
	if s == nil || s.store == nil {
		return ErrStoreUnavailable
	}
	if err := s.store.DeleteRoute(strings.TrimSpace(alias)); err != nil {
		return err
	}
	if s.logger != nil {
		s.logger.Info("model route deleted", "alias", alias)
	}
	return nil
}

func (s *Service) CheckHealth(ctx context.Context, upstreamID string) ([]HealthCheckResult, error) {
	if s == nil || s.store == nil {
		return nil, ErrStoreUnavailable
	}

	upstreams, err := s.store.ListUpstreams()
	if err != nil {
		return nil, err
	}

	targetID := strings.TrimSpace(upstreamID)
	results := make([]HealthCheckResult, 0, len(upstreams))
	for _, up := range upstreams {
		if targetID != "" && up.ID != targetID {
			continue
		}
		result := checkSingleUpstream(ctx, up)
		up.LastHealthCheckedAt = result.CheckedAt
		up.LastHealthLatency = result.LatencyMS
		up.LastHealthHTTPCode = result.HTTPCode
		up.LastHealthEndpoint = result.Endpoint
		up.LastHealthResponse = result.Response
		if result.Healthy {
			up.LastHealthStatus = "healthy"
			up.LastHealthError = ""
		} else {
			up.LastHealthStatus = "unhealthy"
			up.LastHealthError = result.Error
		}
		up.UpdatedAt = time.Now().UTC()
		if _, err := s.store.UpsertUpstream(up); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if targetID != "" && len(results) == 0 {
		return nil, ErrUpstreamNotFound
	}
	return results, nil
}

func (s *Service) RecordRuntime(modelAlias, upstreamID string, success bool, latency time.Duration, err error) {
	if s == nil {
		return
	}
	modelAlias = strings.TrimSpace(modelAlias)
	upstreamID = strings.TrimSpace(upstreamID)
	if modelAlias == "" || upstreamID == "" {
		return
	}
	key := modelAlias + "|" + upstreamID

	s.statsMu.Lock()
	defer s.statsMu.Unlock()

	stat := s.stats[key]
	stat.ModelAlias = modelAlias
	stat.UpstreamID = upstreamID
	if success {
		stat.SuccessCount++
		stat.LastError = ""
	} else {
		stat.FailureCount++
		if err != nil {
			stat.LastError = err.Error()
		}
	}
	stat.LastLatencyMS = latency.Milliseconds()
	stat.LastSeenAt = time.Now().UTC()
	s.stats[key] = stat
}

func (s *Service) RuntimeStats() []RuntimeStat {
	if s == nil {
		return nil
	}
	s.statsMu.Lock()
	defer s.statsMu.Unlock()

	out := make([]RuntimeStat, 0, len(s.stats))
	for _, stat := range s.stats {
		out = append(out, stat)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ModelAlias == out[j].ModelAlias {
			return out[i].UpstreamID < out[j].UpstreamID
		}
		return out[i].ModelAlias < out[j].ModelAlias
	})
	return out
}

func (s *Service) Resolve(modelAlias string) ([]ResolvedTarget, bool, error) {
	if s == nil || s.store == nil {
		return nil, false, ErrStoreUnavailable
	}
	modelAlias = strings.TrimSpace(modelAlias)
	if modelAlias == "" {
		return nil, false, nil
	}

	upstreams, routes, err := s.store.Snapshot()
	if err != nil {
		return nil, false, err
	}

	var route *ModelRoute
	for i := range routes {
		if strings.EqualFold(routes[i].Alias, modelAlias) {
			route = &routes[i]
			break
		}
	}
	if route == nil {
		return nil, false, nil
	}
	if !route.Enabled {
		return nil, true, ErrRouteDisabled
	}

	upstreamMap := make(map[string]Upstream, len(upstreams))
	for _, up := range upstreams {
		upstreamMap[up.ID] = up
	}

	targets := make([]ResolvedTarget, 0, len(route.Targets))
	for _, target := range route.Targets {
		if !target.Enabled {
			continue
		}
		up, ok := upstreamMap[target.UpstreamID]
		if !ok || !up.Enabled {
			continue
		}
		resolvedModel := strings.TrimSpace(target.Model)
		if resolvedModel == "" {
			resolvedModel = route.Alias
		}
		targets = append(targets, ResolvedTarget{
			UpstreamID:   up.ID,
			UpstreamName: up.Name,
			ProviderType: up.Type,
			BaseURL:      up.BaseURL,
			APIKey:       up.APIKey,
			Model:        resolvedModel,
		})
	}
	if len(targets) == 0 {
		return nil, true, ErrNoAvailableTarget
	}

	ordered := orderedTargets(route.Strategy, route.Targets, targets)
	return ordered, true, nil
}

func normalizeUpstream(req UpsertUpstreamRequest) (Upstream, error) {
	up := Upstream{
		ID:      strings.TrimSpace(req.ID),
		Name:    strings.TrimSpace(req.Name),
		Type:    normalizeUpstreamType(req.Type),
		BaseURL: normalizeURL(req.BaseURL),
		APIKey:  strings.TrimSpace(req.APIKey),
		Models:  normalizeModelList(req.Models),
		Enabled: req.Enabled,
	}
	if up.Name == "" || up.Type == "" || up.BaseURL == "" {
		return Upstream{}, ErrInvalidUpstream
	}
	if _, err := url.ParseRequestURI(up.BaseURL); err != nil {
		return Upstream{}, ErrInvalidUpstream
	}
	return up, nil
}

func normalizeModelList(items []string) []string {
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

func normalizeRoute(req UpsertRouteRequest) (ModelRoute, error) {
	alias := strings.TrimSpace(req.Alias)
	strategy := normalizeStrategy(req.Strategy)
	if alias == "" || strategy == "" {
		return ModelRoute{}, ErrInvalidRoute
	}
	targets := make([]RouteTarget, 0, len(req.Targets))
	for _, t := range req.Targets {
		upstreamID := strings.TrimSpace(t.UpstreamID)
		model := strings.TrimSpace(t.Model)
		if upstreamID == "" {
			return ModelRoute{}, ErrInvalidRoute
		}
		weight := t.Weight
		if weight < 0 {
			return ModelRoute{}, ErrInvalidRoute
		}
		priority := t.Priority
		if priority < 0 {
			return ModelRoute{}, ErrInvalidRoute
		}
		targets = append(targets, RouteTarget{
			UpstreamID: upstreamID,
			Model:      model,
			Weight:     weight,
			Priority:   priority,
			Enabled:    t.Enabled,
		})
	}
	if len(targets) == 0 {
		return ModelRoute{}, ErrInvalidRoute
	}
	return ModelRoute{
		Alias:    alias,
		Strategy: strategy,
		Enabled:  req.Enabled,
		Targets:  targets,
	}, nil
}

func normalizeUpstreamType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ProviderOpenAICompat:
		return ProviderOpenAICompat
	case ProviderAnthropic:
		return ProviderAnthropic
	case ProviderGemini:
		return ProviderGemini
	case ProviderOllama:
		return ProviderOllama
	default:
		return ""
	}
}

func normalizeStrategy(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case StrategyFallback:
		return StrategyFallback
	case StrategyWeighted:
		return StrategyWeighted
	default:
		return ""
	}
}

func normalizeURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func orderedTargets(strategy string, routeTargets []RouteTarget, candidates []ResolvedTarget) []ResolvedTarget {
	byUpstream := make(map[string][]RouteTarget)
	for _, t := range routeTargets {
		byUpstream[t.UpstreamID] = append(byUpstream[t.UpstreamID], t)
	}

	items := make([]candidateWithMeta, 0, len(candidates))
	for _, c := range candidates {
		weight := 1
		priority := 1000
		for _, rt := range byUpstream[c.UpstreamID] {
			if strings.TrimSpace(rt.Model) == strings.TrimSpace(c.Model) || strings.TrimSpace(rt.Model) == "" {
				if rt.Weight > 0 {
					weight = rt.Weight
				}
				priority = rt.Priority
				break
			}
		}
		items = append(items, candidateWithMeta{target: c, weight: weight, priority: priority})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].priority == items[j].priority {
			return items[i].weight > items[j].weight
		}
		return items[i].priority < items[j].priority
	})

	if strategy != StrategyWeighted || len(items) <= 1 {
		out := make([]ResolvedTarget, 0, len(items))
		for _, it := range items {
			out = append(out, it.target)
		}
		return out
	}

	total := 0
	for _, it := range items {
		if it.weight > 0 {
			total += it.weight
		}
	}
	if total <= 0 {
		out := make([]ResolvedTarget, 0, len(items))
		for _, it := range items {
			out = append(out, it.target)
		}
		return out
	}

	selected := weightedPick(items, total)
	out := make([]ResolvedTarget, 0, len(items))
	out = append(out, items[selected].target)
	for i := range items {
		if i == selected {
			continue
		}
		out = append(out, items[i].target)
	}
	return out
}

func weightedPick(items []candidateWithMeta, total int) int {
	draw := randomInt(total)
	running := 0
	for idx, it := range items {
		if it.weight <= 0 {
			continue
		}
		running += it.weight
		if draw < running {
			return idx
		}
	}
	return 0
}

func randomInt(max int) int {
	if max <= 1 {
		return 0
	}
	var buf [8]byte
	if _, err := crand.Read(buf[:]); err != nil {
		return 0
	}
	n := binary.BigEndian.Uint64(buf[:])
	return int(n % uint64(max))
}

func checkSingleUpstream(ctx context.Context, up Upstream) HealthCheckResult {
	start := time.Now()
	res := HealthCheckResult{
		UpstreamID: up.ID,
		CheckedAt:  start.UTC(),
	}

	if !up.Enabled {
		res.Healthy = false
		res.Error = "upstream disabled"
		return finalizeHealthResult(start, res)
	}

	client := &http.Client{Timeout: 6 * time.Second}
	checkURL, err := resolveHealthURL(up)
	if err != nil {
		res.Healthy = false
		res.Error = err.Error()
		return finalizeHealthResult(start, res)
	}
	res.Endpoint = checkURL
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		res.Healthy = false
		res.Error = err.Error()
		return finalizeHealthResult(start, res)
	}
	if strings.TrimSpace(up.APIKey) != "" {
		if up.Type == ProviderAnthropic {
			req.Header.Set("x-api-key", strings.TrimSpace(up.APIKey))
			req.Header.Set("anthropic-version", "2023-06-01")
		} else if up.Type == ProviderGemini {
			req.Header.Set("x-goog-api-key", strings.TrimSpace(up.APIKey))
		} else if up.Type == ProviderOpenAICompat {
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(up.APIKey))
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		res.Healthy = false
		res.Error = err.Error()
		return finalizeHealthResult(start, res)
	}
	defer resp.Body.Close()
	res.HTTPCode = resp.StatusCode

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		res.Healthy = false
		res.Error = err.Error()
		return finalizeHealthResult(start, res)
	}
	res.Response = normalizeHealthResponse(raw, 2000)

	// Health = reachability: as long as request/response succeeds, treat upstream as healthy.
	res.Healthy = true
	return finalizeHealthResult(start, res)
}

func resolveHealthURL(up Upstream) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(up.BaseURL), "/")
	if base == "" {
		return "", ErrInvalidUpstream
	}
	switch up.Type {
	case ProviderOpenAICompat:
		if strings.HasSuffix(base, "/v1") {
			return base + "/models", nil
		}
		return base + "/v1/models", nil
	case ProviderAnthropic:
		if strings.HasSuffix(base, "/v1") {
			return base + "/models", nil
		}
		return base + "/v1/models", nil
	case ProviderGemini:
		if strings.HasSuffix(base, "/v1") || strings.HasSuffix(base, "/v1beta") {
			return base + "/models", nil
		}
		return base + "/v1beta/models", nil
	case ProviderOllama:
		return base + "/api/tags", nil
	default:
		return "", ErrInvalidUpstream
	}
}

func finalizeHealthResult(start time.Time, res HealthCheckResult) HealthCheckResult {
	d := time.Since(start)
	res.Duration = d
	res.LatencyMS = d.Milliseconds()
	return res
}

func normalizeHealthResponse(raw []byte, limit int) string {
	v := strings.TrimSpace(string(raw))
	if limit <= 0 || len(v) <= limit {
		return v
	}
	return strings.TrimSpace(v[:limit]) + "...(truncated)"
}
