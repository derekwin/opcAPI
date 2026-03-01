package audit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type dataset struct {
	Events []Event `json:"events"`
}

type FileStore struct {
	path string
	mu   sync.Mutex
}

func NewFileStore(path string) (*FileStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("audit store path is empty")
	}
	s := &FileStore{path: path}
	if err := s.ensureFile(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileStore) Append(event Event) (Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return Event{}, err
	}
	ds.Events = append(ds.Events, event)
	if err := s.saveLocked(ds); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (s *FileStore) Query(q Query) ([]Event, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return nil, 0, err
	}

	resource := strings.TrimSpace(strings.ToLower(q.Resource))
	action := strings.TrimSpace(strings.ToLower(q.Action))
	actor := strings.TrimSpace(q.ActorUserID)
	targetTenant := strings.TrimSpace(q.TargetTenantID)

	items := make([]Event, 0)
	for _, ev := range ds.Events {
		if resource != "" && strings.ToLower(ev.Resource) != resource {
			continue
		}
		if action != "" && strings.ToLower(ev.Action) != action {
			continue
		}
		if actor != "" && ev.ActorUserID != actor {
			continue
		}
		if targetTenant != "" && ev.TargetTenantID != targetTenant {
			continue
		}
		items = append(items, ev)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})

	total := len(items)
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []Event{}, total, nil
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	end := offset + limit
	if end > total {
		end = total
	}

	return items[offset:end], total, nil
}

func (s *FileStore) ensureFile() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(s.path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	empty := dataset{Events: []Event{}}
	b, err := json.MarshalIndent(empty, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(s.path, b, 0o644)
}

func (s *FileStore) loadLocked() (dataset, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return dataset{}, err
	}
	if len(b) == 0 {
		return dataset{Events: []Event{}}, nil
	}
	var ds dataset
	if err := json.Unmarshal(b, &ds); err != nil {
		return dataset{}, err
	}
	if ds.Events == nil {
		ds.Events = []Event{}
	}
	return ds, nil
}

func (s *FileStore) saveLocked(ds dataset) error {
	b, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
