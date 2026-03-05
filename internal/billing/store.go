package billing

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type dataset struct {
	Balances      []Balance      `json:"balances"`
	Orders        []Order        `json:"orders"`
	Subscriptions []Subscription `json:"subscriptions"`
	PlanConfigs   []PlanConfig   `json:"plan_configs,omitempty"`
}

type FileStore struct {
	path string
	mu   sync.Mutex
}

func NewFileStore(path string) (*FileStore, error) {
	if path == "" {
		return nil, errors.New("billing store path is empty")
	}
	s := &FileStore{path: path}
	if err := s.ensureFile(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileStore) GetOrCreateBalance(userID, currency string) (Balance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return Balance{}, err
	}

	for _, b := range ds.Balances {
		if normalizeSubjectID(b.UserID, b.TenantID) == userID {
			b.UserID = normalizeSubjectID(b.UserID, b.TenantID)
			if target := strings.TrimSpace(currency); target != "" && b.Currency != target {
				b.Currency = target
				b.UpdatedAt = time.Now().UTC()
				for i := range ds.Balances {
					if normalizeSubjectID(ds.Balances[i].UserID, ds.Balances[i].TenantID) == userID {
						ds.Balances[i] = b
						break
					}
				}
				if err := s.saveLocked(ds); err != nil {
					return Balance{}, err
				}
			}
			return b, nil
		}
	}

	bal := Balance{UserID: userID, Amount: 0, Currency: currency, UpdatedAt: time.Now().UTC()}
	ds.Balances = append(ds.Balances, bal)
	if err := s.saveLocked(ds); err != nil {
		return Balance{}, err
	}
	return bal, nil
}

func (s *FileStore) AddBalance(userID string, amount float64, currency string) (Balance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return Balance{}, err
	}

	for i, b := range ds.Balances {
		if normalizeSubjectID(b.UserID, b.TenantID) == userID {
			b.UserID = userID
			b.Amount += amount
			b.Currency = currency
			b.UpdatedAt = time.Now().UTC()
			ds.Balances[i] = b
			if err := s.saveLocked(ds); err != nil {
				return Balance{}, err
			}
			return b, nil
		}
	}

	b := Balance{UserID: userID, Amount: amount, Currency: currency, UpdatedAt: time.Now().UTC()}
	ds.Balances = append(ds.Balances, b)
	if err := s.saveLocked(ds); err != nil {
		return Balance{}, err
	}
	return b, nil
}

func (s *FileStore) DeductBalance(userID string, amount float64, currency string) (Balance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return Balance{}, err
	}

	if amount <= 0 {
		return Balance{}, ErrInvalidAmount
	}

	for i, b := range ds.Balances {
		if normalizeSubjectID(b.UserID, b.TenantID) != userID {
			continue
		}
		if b.Currency != "" && currency != "" && b.Currency != currency {
			return Balance{}, ErrInvalidCurrency
		}
		if b.Amount < amount-1e-9 {
			return Balance{}, ErrInsufficientBalance
		}

		b.UserID = userID
		b.Amount -= amount
		if b.Amount < 0 && b.Amount > -1e-9 {
			b.Amount = 0
		}
		b.UpdatedAt = time.Now().UTC()
		ds.Balances[i] = b

		if err := s.saveLocked(ds); err != nil {
			return Balance{}, err
		}
		return b, nil
	}

	return Balance{}, ErrInsufficientBalance
}

func (s *FileStore) CreateOrder(order Order) (Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return Order{}, err
	}
	ds.Orders = append(ds.Orders, order)
	if err := s.saveLocked(ds); err != nil {
		return Order{}, err
	}
	return order, nil
}

func (s *FileStore) CreateSubscription(sub Subscription) (Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return Subscription{}, err
	}
	ds.Subscriptions = append(ds.Subscriptions, sub)
	if err := s.saveLocked(ds); err != nil {
		return Subscription{}, err
	}
	return sub, nil
}

func (s *FileStore) ListSubscriptions() ([]Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	out := make([]Subscription, 0, len(ds.Subscriptions))
	for _, sub := range ds.Subscriptions {
		sub.UserID = normalizeSubjectID(sub.UserID, sub.TenantID)
		out = append(out, sub)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (s *FileStore) ListPlanConfigs() ([]PlanConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	out := make([]PlanConfig, 0, len(ds.PlanConfigs))
	for _, cfg := range ds.PlanConfigs {
		cfg.PlanID = strings.ToLower(strings.TrimSpace(cfg.PlanID))
		if cfg.PlanID == "" {
			continue
		}
		out = append(out, cfg)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PlanID == out[j].PlanID {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].PlanID < out[j].PlanID
	})
	return out, nil
}

func (s *FileStore) SavePlanConfigs(configs []PlanConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return err
	}
	ds.PlanConfigs = make([]PlanConfig, 0, len(configs))
	for _, cfg := range configs {
		cfg.PlanID = strings.ToLower(strings.TrimSpace(cfg.PlanID))
		if cfg.PlanID == "" {
			continue
		}
		ds.PlanConfigs = append(ds.PlanConfigs, cfg)
	}
	return s.saveLocked(ds)
}

func (s *FileStore) ListBalances() ([]Balance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return nil, err
	}

	out := make([]Balance, 0, len(ds.Balances))
	for _, b := range ds.Balances {
		b.UserID = normalizeSubjectID(b.UserID, b.TenantID)
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (s *FileStore) ListOrders(userID string, limit int) ([]Order, error) {
	orders, _, err := s.QueryOrders(OrderQuery{UserID: userID, Limit: limit})
	return orders, err
}

func (s *FileStore) QueryOrders(query OrderQuery) ([]Order, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return nil, 0, err
	}

	orders := make([]Order, 0)
	orderID := strings.TrimSpace(query.ID)
	userID := strings.TrimSpace(query.UserID)
	if userID == "" {
		userID = strings.TrimSpace(query.TenantID)
	}
	orderType := strings.ToLower(strings.TrimSpace(query.Type))
	status := strings.ToLower(strings.TrimSpace(query.Status))
	channel := strings.ToLower(strings.TrimSpace(query.Channel))

	for _, o := range ds.Orders {
		if orderID != "" && o.ID != orderID {
			continue
		}
		subjectID := normalizeSubjectID(o.UserID, o.TenantID)
		if userID != "" && subjectID != userID {
			continue
		}
		o.UserID = subjectID
		if orderType != "" && strings.ToLower(o.Type) != orderType {
			continue
		}
		if status != "" && strings.ToLower(o.Status) != status {
			continue
		}
		if channel != "" && strings.ToLower(o.Channel) != channel {
			continue
		}
		orders = append(orders, o)
	}

	sort.Slice(orders, func(i, j int) bool {
		return orders[i].CreatedAt.After(orders[j].CreatedAt)
	})

	total := len(orders)
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []Order{}, total, nil
	}

	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	end := offset + limit
	if end > total {
		end = total
	}

	return orders[offset:end], total, nil
}

func (s *FileStore) DeleteOrder(orderID string) (Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return Order{}, err
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return Order{}, ErrOrderNotFound
	}

	idx := -1
	var deleted Order
	for i := range ds.Orders {
		if ds.Orders[i].ID == orderID {
			idx = i
			deleted = ds.Orders[i]
			break
		}
	}
	if idx < 0 {
		return Order{}, ErrOrderNotFound
	}
	ds.Orders = append(ds.Orders[:idx], ds.Orders[idx+1:]...)
	if err := s.saveLocked(ds); err != nil {
		return Order{}, err
	}
	return deleted, nil
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

	empty := dataset{Balances: []Balance{}, Orders: []Order{}, Subscriptions: []Subscription{}, PlanConfigs: []PlanConfig{}}
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
		return dataset{Balances: []Balance{}, Orders: []Order{}, Subscriptions: []Subscription{}, PlanConfigs: []PlanConfig{}}, nil
	}

	var ds dataset
	if err := json.Unmarshal(b, &ds); err != nil {
		return dataset{}, err
	}
	if ds.Balances == nil {
		ds.Balances = []Balance{}
	}
	if ds.Orders == nil {
		ds.Orders = []Order{}
	}
	if ds.Subscriptions == nil {
		ds.Subscriptions = []Subscription{}
	}
	if ds.PlanConfigs == nil {
		ds.PlanConfigs = []PlanConfig{}
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

func normalizeSubjectID(userID, tenantID string) string {
	if strings.TrimSpace(userID) != "" {
		return strings.TrimSpace(userID)
	}
	return strings.TrimSpace(tenantID)
}
