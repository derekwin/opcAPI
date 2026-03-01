package user

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type fileDataset struct {
	Users       []User       `json:"users"`
	Tenants     []Tenant     `json:"tenants"`
	Memberships []Membership `json:"memberships"`
}

type FileStore struct {
	path string
	mu   sync.Mutex
}

func NewFileStore(path string) (*FileStore, error) {
	if path == "" {
		return nil, errors.New("user store path is empty")
	}
	s := &FileStore{path: path}
	if err := s.ensureFile(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileStore) CreateTenantOwner(channel Channel, identifier, tenantName string) (User, Tenant, Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return User{}, Tenant{}, Membership{}, err
	}

	if _, ok := findUserByIdentity(ds.Users, channel, identifier); ok {
		return User{}, Tenant{}, Membership{}, ErrUserExists
	}

	now := time.Now().UTC()
	tenant := Tenant{
		ID:        newID("ten_"),
		Name:      fallbackTenantName(identifier, tenantName),
		PlanID:    "free",
		Status:    "active",
		CreatedAt: now,
	}
	user := User{
		ID:        newID("usr_"),
		TenantID:  tenant.ID,
		Status:    "active",
		CreatedAt: now,
	}
	assignIdentity(&user, channel, identifier)

	membership := Membership{
		TenantID: tenant.ID,
		UserID:   user.ID,
		Role:     "owner",
		JoinedAt: now,
	}

	ds.Users = append(ds.Users, user)
	ds.Tenants = append(ds.Tenants, tenant)
	ds.Memberships = append(ds.Memberships, membership)

	if err := s.saveLocked(ds); err != nil {
		return User{}, Tenant{}, Membership{}, err
	}
	return user, tenant, membership, nil
}

func (s *FileStore) CreateTenantUser(tenantID string, channel Channel, identifier, role string) (TenantUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return TenantUser{}, err
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return TenantUser{}, ErrTenantNotFound
	}
	if _, ok := findTenantByID(ds.Tenants, tenantID); !ok {
		return TenantUser{}, ErrTenantNotFound
	}

	if _, ok := findUserByIdentity(ds.Users, channel, identifier); ok {
		return TenantUser{}, ErrUserExists
	}

	now := time.Now().UTC()
	u := User{
		ID:        newID("usr_"),
		TenantID:  tenantID,
		Status:    "active",
		CreatedAt: now,
	}
	assignIdentity(&u, channel, identifier)

	membership := Membership{
		TenantID: tenantID,
		UserID:   u.ID,
		Role:     role,
		JoinedAt: now,
	}

	ds.Users = append(ds.Users, u)
	ds.Memberships = append(ds.Memberships, membership)
	if err := s.saveLocked(ds); err != nil {
		return TenantUser{}, err
	}
	return TenantUser{User: u, Role: role}, nil
}

func (s *FileStore) UpdateTenantUserRole(tenantID, userID, role string) (TenantUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return TenantUser{}, err
	}
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	if tenantID == "" || userID == "" {
		return TenantUser{}, ErrUserNotFound
	}

	idx := -1
	for i, m := range ds.Memberships {
		if m.TenantID == tenantID && m.UserID == userID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return TenantUser{}, ErrUserNotFound
	}

	current := ds.Memberships[idx]
	if current.Role == "owner" && role != "owner" {
		return TenantUser{}, ErrForbidden
	}
	ds.Memberships[idx].Role = role

	if err := s.saveLocked(ds); err != nil {
		return TenantUser{}, err
	}

	u, ok := findUserByID(ds.Users, userID)
	if !ok {
		return TenantUser{}, ErrUserNotFound
	}
	return TenantUser{User: u, Role: role}, nil
}

func (s *FileStore) UpdateTenantUserStatus(tenantID, userID, status string) (TenantUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return TenantUser{}, err
	}
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	if tenantID == "" || userID == "" {
		return TenantUser{}, ErrUserNotFound
	}

	membership, ok := findMembershipByTenantAndUserID(ds.Memberships, tenantID, userID)
	if !ok {
		return TenantUser{}, ErrUserNotFound
	}

	userIdx := -1
	for i, u := range ds.Users {
		if u.ID == userID && u.TenantID == tenantID {
			userIdx = i
			break
		}
	}
	if userIdx < 0 {
		return TenantUser{}, ErrUserNotFound
	}
	ds.Users[userIdx].Status = status

	if err := s.saveLocked(ds); err != nil {
		return TenantUser{}, err
	}
	return TenantUser{User: ds.Users[userIdx], Role: membership.Role}, nil
}

func (s *FileStore) FindUserByIdentity(channel Channel, identifier string) (User, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return User{}, false, err
	}
	u, ok := findUserByIdentity(ds.Users, channel, identifier)
	return u, ok, nil
}

func (s *FileStore) GetSessionContext(userID string) (User, Membership, Tenant, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return User{}, Membership{}, Tenant{}, false, err
	}

	user, ok := findUserByID(ds.Users, userID)
	if !ok {
		return User{}, Membership{}, Tenant{}, false, nil
	}

	membership, ok := findMembershipByUserID(ds.Memberships, userID)
	if !ok {
		return User{}, Membership{}, Tenant{}, false, nil
	}

	tenant, ok := findTenantByID(ds.Tenants, membership.TenantID)
	if !ok {
		return User{}, Membership{}, Tenant{}, false, nil
	}

	return user, membership, tenant, true, nil
}

func (s *FileStore) ListTenantUsers(tenantID string) ([]TenantUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return nil, err
	}

	out := make([]TenantUser, 0)
	for _, m := range ds.Memberships {
		if m.TenantID != tenantID {
			continue
		}
		u, ok := findUserByID(ds.Users, m.UserID)
		if !ok {
			continue
		}
		out = append(out, TenantUser{User: u, Role: m.Role})
	}
	return out, nil
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

	empty := fileDataset{Users: []User{}, Tenants: []Tenant{}, Memberships: []Membership{}}
	b, err := json.MarshalIndent(empty, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(s.path, b, 0o644)
}

func (s *FileStore) loadLocked() (fileDataset, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return fileDataset{}, err
	}
	if len(b) == 0 {
		return fileDataset{Users: []User{}, Tenants: []Tenant{}, Memberships: []Membership{}}, nil
	}

	var ds fileDataset
	if err := json.Unmarshal(b, &ds); err != nil {
		return fileDataset{}, err
	}
	if ds.Users == nil {
		ds.Users = []User{}
	}
	if ds.Tenants == nil {
		ds.Tenants = []Tenant{}
	}
	if ds.Memberships == nil {
		ds.Memberships = []Membership{}
	}
	return ds, nil
}

func (s *FileStore) saveLocked(ds fileDataset) error {
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

func findUserByIdentity(users []User, channel Channel, identifier string) (User, bool) {
	for _, u := range users {
		switch channel {
		case ChannelPhone:
			if u.Phone == identifier {
				return u, true
			}
		case ChannelWeChat:
			if u.WeChatOpenID == identifier {
				return u, true
			}
		case ChannelFeishu:
			if u.FeishuOpenID == identifier {
				return u, true
			}
		}
	}
	return User{}, false
}

func findUserByID(users []User, userID string) (User, bool) {
	for _, u := range users {
		if u.ID == userID {
			return u, true
		}
	}
	return User{}, false
}

func findMembershipByUserID(memberships []Membership, userID string) (Membership, bool) {
	for _, m := range memberships {
		if m.UserID == userID {
			return m, true
		}
	}
	return Membership{}, false
}

func findMembershipByTenantAndUserID(memberships []Membership, tenantID, userID string) (Membership, bool) {
	for _, m := range memberships {
		if m.TenantID == tenantID && m.UserID == userID {
			return m, true
		}
	}
	return Membership{}, false
}

func findTenantByID(tenants []Tenant, tenantID string) (Tenant, bool) {
	for _, t := range tenants {
		if t.ID == tenantID {
			return t, true
		}
	}
	return Tenant{}, false
}

func assignIdentity(user *User, channel Channel, identifier string) {
	switch channel {
	case ChannelPhone:
		user.Phone = identifier
	case ChannelWeChat:
		user.WeChatOpenID = identifier
	case ChannelFeishu:
		user.FeishuOpenID = identifier
	}
}

func fallbackTenantName(identifier, tenantName string) string {
	if tenantName != "" {
		return tenantName
	}
	if len(identifier) > 8 {
		identifier = identifier[:8]
	}
	return fmt.Sprintf("opc-%s", identifier)
}
