package promptapi

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
	Definitions []Definition `json:"definitions"`
}

type FileStore struct {
	path string
	mu   sync.Mutex
}

func NewFileStore(path string) (*FileStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("prompt api store path is empty")
	}
	s := &FileStore{path: path}
	if err := s.ensureFile(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileStore) Get(tenantID, slug string) (Definition, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return Definition{}, false, err
	}
	for _, d := range ds.Definitions {
		if d.TenantID == tenantID && d.Slug == slug {
			return d, true, nil
		}
	}
	return Definition{}, false, nil
}

func (s *FileStore) Upsert(def Definition) (Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return Definition{}, err
	}

	replaced := false
	for i := range ds.Definitions {
		if ds.Definitions[i].TenantID == def.TenantID && ds.Definitions[i].Slug == def.Slug {
			ds.Definitions[i] = def
			replaced = true
			break
		}
	}
	if !replaced {
		ds.Definitions = append(ds.Definitions, def)
	}

	if err := s.saveLocked(ds); err != nil {
		return Definition{}, err
	}
	return def, nil
}

func (s *FileStore) Query(q ListQuery) ([]Definition, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return nil, 0, err
	}

	status := strings.ToLower(strings.TrimSpace(q.Status))
	items := make([]Definition, 0)
	for _, d := range ds.Definitions {
		if d.TenantID != q.TenantID {
			continue
		}
		if status != "" && strings.ToLower(d.Status) != status {
			continue
		}
		items = append(items, d)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})

	total := len(items)
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []Definition{}, total, nil
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

	empty := dataset{Definitions: []Definition{}}
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
		return dataset{Definitions: []Definition{}}, nil
	}

	var ds dataset
	if err := json.Unmarshal(b, &ds); err != nil {
		return dataset{}, err
	}
	if ds.Definitions == nil {
		ds.Definitions = []Definition{}
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
