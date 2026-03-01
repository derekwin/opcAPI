package audit

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

type Service struct {
	store  *FileStore
	logger *slog.Logger
}

func NewService(store *FileStore, logger *slog.Logger) *Service {
	return &Service{store: store, logger: logger}
}

func (s *Service) Record(_ context.Context, input RecordInput) (Event, error) {
	if s.store == nil {
		return Event{}, ErrStoreUnavailable
	}
	resource := strings.TrimSpace(strings.ToLower(input.Resource))
	action := strings.TrimSpace(strings.ToLower(input.Action))
	if resource == "" || action == "" {
		return Event{}, ErrInvalidEvent
	}

	ev := Event{
		ID:             newID("aud_"),
		Resource:       resource,
		Action:         action,
		ActorUserID:    strings.TrimSpace(input.ActorUserID),
		ActorTenantID:  strings.TrimSpace(input.ActorTenantID),
		TargetID:       strings.TrimSpace(input.TargetID),
		TargetTenantID: strings.TrimSpace(input.TargetTenantID),
		Metadata:       normalizeMetadata(input.Metadata),
		CreatedAt:      time.Now().UTC(),
	}
	res, err := s.store.Append(ev)
	if err != nil {
		return Event{}, err
	}
	if s.logger != nil {
		s.logger.Info("audit event", "resource", res.Resource, "action", res.Action, "actor_user_id", res.ActorUserID, "target_id", res.TargetID)
	}
	return res, nil
}

func (s *Service) List(_ context.Context, q Query) (Page, error) {
	if s.store == nil {
		return Page{}, ErrStoreUnavailable
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

	items, total, err := s.store.Query(Query{
		Resource:       q.Resource,
		Action:         q.Action,
		ActorUserID:    q.ActorUserID,
		TargetTenantID: q.TargetTenantID,
		Limit:          limit,
		Offset:         offset,
	})
	if err != nil {
		return Page{}, err
	}
	return Page{Data: items, Total: total, Limit: limit, Offset: offset}, nil
}

func normalizeMetadata(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for k, v := range input {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
