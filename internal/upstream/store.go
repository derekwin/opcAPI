package upstream

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type fileDataset struct {
	Upstreams []Upstream   `json:"upstreams"`
	Routes    []ModelRoute `json:"routes"`
}

type FileStore struct {
	path string
	mu   sync.Mutex
}

func NewFileStore(path string) (*FileStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("upstream store path is empty")
	}
	s := &FileStore{path: path}
	if err := s.ensureFile(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileStore) ListUpstreams() ([]Upstream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	out := make([]Upstream, len(ds.Upstreams))
	copy(out, ds.Upstreams)
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (s *FileStore) UpsertUpstream(up Upstream) (Upstream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return Upstream{}, err
	}

	now := time.Now().UTC()
	up.ID = strings.TrimSpace(up.ID)
	if up.ID == "" {
		up.ID = newID("ups_")
		up.CreatedAt = now
	}
	up.UpdatedAt = now

	for i := range ds.Upstreams {
		if ds.Upstreams[i].ID == up.ID {
			up.CreatedAt = ds.Upstreams[i].CreatedAt
			ds.Upstreams[i] = up
			if err := s.saveLocked(ds); err != nil {
				return Upstream{}, err
			}
			return up, nil
		}
	}

	ds.Upstreams = append(ds.Upstreams, up)
	if err := s.saveLocked(ds); err != nil {
		return Upstream{}, err
	}
	return up, nil
}

func (s *FileStore) DeleteUpstream(upstreamID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return err
	}
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return ErrUpstreamNotFound
	}

	idx := -1
	for i := range ds.Upstreams {
		if ds.Upstreams[i].ID == upstreamID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrUpstreamNotFound
	}
	ds.Upstreams = append(ds.Upstreams[:idx], ds.Upstreams[idx+1:]...)

	now := time.Now().UTC()
	for i := range ds.Routes {
		filtered := make([]RouteTarget, 0, len(ds.Routes[i].Targets))
		for _, t := range ds.Routes[i].Targets {
			if strings.TrimSpace(t.UpstreamID) == upstreamID {
				continue
			}
			filtered = append(filtered, t)
		}
		ds.Routes[i].Targets = filtered
		ds.Routes[i].UpdatedAt = now
	}
	return s.saveLocked(ds)
}

func (s *FileStore) ListRoutes() ([]ModelRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	out := make([]ModelRoute, len(ds.Routes))
	copy(out, ds.Routes)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Alias < out[j].Alias
	})
	return out, nil
}

func (s *FileStore) UpsertRoute(route ModelRoute) (ModelRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return ModelRoute{}, err
	}

	now := time.Now().UTC()
	route.Alias = strings.TrimSpace(route.Alias)
	route.UpdatedAt = now
	for i := range ds.Routes {
		if strings.EqualFold(ds.Routes[i].Alias, route.Alias) {
			route.CreatedAt = ds.Routes[i].CreatedAt
			ds.Routes[i] = route
			if err := s.saveLocked(ds); err != nil {
				return ModelRoute{}, err
			}
			return route, nil
		}
	}

	route.CreatedAt = now
	ds.Routes = append(ds.Routes, route)
	if err := s.saveLocked(ds); err != nil {
		return ModelRoute{}, err
	}
	return route, nil
}

func (s *FileStore) DeleteRoute(alias string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return err
	}
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return ErrRouteNotFound
	}
	idx := -1
	for i := range ds.Routes {
		if strings.EqualFold(ds.Routes[i].Alias, alias) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrRouteNotFound
	}
	ds.Routes = append(ds.Routes[:idx], ds.Routes[idx+1:]...)
	return s.saveLocked(ds)
}

func (s *FileStore) Snapshot() ([]Upstream, []ModelRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return nil, nil, err
	}
	upstreams := make([]Upstream, len(ds.Upstreams))
	copy(upstreams, ds.Upstreams)
	routes := make([]ModelRoute, len(ds.Routes))
	copy(routes, ds.Routes)
	return upstreams, routes, nil
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

	empty := fileDataset{Upstreams: []Upstream{}, Routes: []ModelRoute{}}
	buf, err := json.MarshalIndent(empty, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	return os.WriteFile(s.path, buf, 0o644)
}

func (s *FileStore) loadLocked() (fileDataset, error) {
	buf, err := os.ReadFile(s.path)
	if err != nil {
		return fileDataset{}, err
	}
	if len(buf) == 0 {
		return fileDataset{Upstreams: []Upstream{}, Routes: []ModelRoute{}}, nil
	}
	var ds fileDataset
	if err := json.Unmarshal(buf, &ds); err != nil {
		return fileDataset{}, err
	}
	if ds.Upstreams == nil {
		ds.Upstreams = []Upstream{}
	}
	if ds.Routes == nil {
		ds.Routes = []ModelRoute{}
	}
	return ds, nil
}

func (s *FileStore) saveLocked(ds fileDataset) error {
	buf, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func newID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return prefix + time.Now().UTC().Format("20060102150405")
	}
	return prefix + hex.EncodeToString(buf)
}
