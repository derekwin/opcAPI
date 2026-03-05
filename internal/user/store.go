package user

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

type userRecord struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"password_hash"`
	PasswordSalt string    `json:"password_salt"`
	Role         string    `json:"role"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type revokedSession struct {
	JTI       string    `json:"jti"`
	Exp       int64     `json:"exp"`
	RevokedAt time.Time `json:"revoked_at"`
}

type credential struct {
	User         User
	PasswordHash string
	PasswordSalt string
}

type fileDataset struct {
	Users           []userRecord     `json:"users"`
	RevokedSessions []revokedSession `json:"revoked_sessions"`
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

func (s *FileStore) CreateUser(email, passwordHash, passwordSalt, role string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return User{}, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if _, ok, _ := findCredentialByEmail(ds.Users, email); ok {
		return User{}, ErrUserExists
	}

	now := time.Now().UTC()
	rec := userRecord{
		ID:           newID("usr_"),
		Email:        email,
		PasswordHash: strings.TrimSpace(passwordHash),
		PasswordSalt: strings.TrimSpace(passwordSalt),
		Role:         strings.ToLower(strings.TrimSpace(role)),
		Status:       "active",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	ds.Users = append(ds.Users, rec)
	if err := s.saveLocked(ds); err != nil {
		return User{}, err
	}
	return toUser(rec), nil
}

func (s *FileStore) FindCredentialByEmail(email string) (credential, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return credential{}, false, err
	}
	return findCredentialByEmail(ds.Users, strings.ToLower(strings.TrimSpace(email)))
}

func (s *FileStore) GetUserByID(userID string) (User, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return User{}, false, err
	}
	for _, rec := range ds.Users {
		if rec.ID == userID {
			return toUser(rec), true, nil
		}
	}
	return User{}, false, nil
}

func (s *FileStore) ListUsers() ([]TenantUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	users := make([]userRecord, len(ds.Users))
	copy(users, ds.Users)
	sort.Slice(users, func(i, j int) bool {
		return users[i].CreatedAt.Before(users[j].CreatedAt)
	})
	out := make([]TenantUser, 0, len(users))
	for _, rec := range users {
		u := toUser(rec)
		out = append(out, TenantUser{User: u, Role: u.Role})
	}
	return out, nil
}

func (s *FileStore) UpdateUserRole(userID, role string) (TenantUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return TenantUser{}, err
	}

	idx := indexOfUser(ds.Users, strings.TrimSpace(userID))
	if idx < 0 {
		return TenantUser{}, ErrUserNotFound
	}
	ds.Users[idx].Role = strings.ToLower(strings.TrimSpace(role))
	ds.Users[idx].UpdatedAt = time.Now().UTC()
	if err := s.saveLocked(ds); err != nil {
		return TenantUser{}, err
	}
	u := toUser(ds.Users[idx])
	return TenantUser{User: u, Role: u.Role}, nil
}

func (s *FileStore) UpdateUserStatus(userID, status string) (TenantUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return TenantUser{}, err
	}

	idx := indexOfUser(ds.Users, strings.TrimSpace(userID))
	if idx < 0 {
		return TenantUser{}, ErrUserNotFound
	}
	ds.Users[idx].Status = strings.ToLower(strings.TrimSpace(status))
	ds.Users[idx].UpdatedAt = time.Now().UTC()
	if err := s.saveLocked(ds); err != nil {
		return TenantUser{}, err
	}
	u := toUser(ds.Users[idx])
	return TenantUser{User: u, Role: u.Role}, nil
}

func (s *FileStore) DeleteUser(userID string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return User{}, err
	}

	idx := indexOfUser(ds.Users, strings.TrimSpace(userID))
	if idx < 0 {
		return User{}, ErrUserNotFound
	}
	deleted := toUser(ds.Users[idx])
	ds.Users = append(ds.Users[:idx], ds.Users[idx+1:]...)
	if err := s.saveLocked(ds); err != nil {
		return User{}, err
	}
	return deleted, nil
}

func (s *FileStore) UserCount() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return 0, err
	}
	return len(ds.Users), nil
}

func (s *FileStore) AnyAdminCount() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, rec := range ds.Users {
		if strings.ToLower(strings.TrimSpace(rec.Role)) == "admin" {
			count++
		}
	}
	return count, nil
}

func (s *FileStore) AdminCount() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, rec := range ds.Users {
		if strings.ToLower(strings.TrimSpace(rec.Role)) == "admin" && strings.ToLower(strings.TrimSpace(rec.Status)) == "active" {
			count++
		}
	}
	return count, nil
}

func (s *FileStore) CreateInitialAdmin(email, passwordHash, passwordSalt string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, err := s.loadLocked()
	if err != nil {
		return User{}, err
	}

	email = strings.ToLower(strings.TrimSpace(email))
	if _, ok, _ := findCredentialByEmail(ds.Users, email); ok {
		return User{}, ErrUserExists
	}
	for _, rec := range ds.Users {
		if strings.ToLower(strings.TrimSpace(rec.Role)) == "admin" {
			return User{}, ErrAdminBootstrapClosed
		}
	}

	now := time.Now().UTC()
	rec := userRecord{
		ID:           newID("usr_"),
		Email:        email,
		PasswordHash: strings.TrimSpace(passwordHash),
		PasswordSalt: strings.TrimSpace(passwordSalt),
		Role:         "admin",
		Status:       "active",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	ds.Users = append(ds.Users, rec)
	if err := s.saveLocked(ds); err != nil {
		return User{}, err
	}
	return toUser(rec), nil
}

func (s *FileStore) RevokeSession(jti string, exp int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	jti = strings.TrimSpace(jti)
	if jti == "" {
		return ErrSessionTokenInvalid
	}
	ds, err := s.loadLocked()
	if err != nil {
		return err
	}
	for _, item := range ds.RevokedSessions {
		if item.JTI == jti {
			return nil
		}
	}
	ds.RevokedSessions = append(ds.RevokedSessions, revokedSession{JTI: jti, Exp: exp, RevokedAt: time.Now().UTC()})
	return s.saveLocked(ds)
}

func (s *FileStore) IsSessionRevoked(jti string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	jti = strings.TrimSpace(jti)
	if jti == "" {
		return true, nil
	}
	ds, err := s.loadLocked()
	if err != nil {
		return false, err
	}
	for _, item := range ds.RevokedSessions {
		if item.JTI == jti {
			return true, nil
		}
	}
	return false, nil
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

	empty := fileDataset{Users: []userRecord{}, RevokedSessions: []revokedSession{}}
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
		return fileDataset{Users: []userRecord{}, RevokedSessions: []revokedSession{}}, nil
	}

	var ds fileDataset
	if err := json.Unmarshal(b, &ds); err != nil {
		return fileDataset{}, err
	}
	if ds.Users == nil {
		ds.Users = []userRecord{}
	}
	if ds.RevokedSessions == nil {
		ds.RevokedSessions = []revokedSession{}
	}

	now := time.Now().UTC().Unix()
	filtered := ds.RevokedSessions[:0]
	for _, item := range ds.RevokedSessions {
		if item.Exp <= 0 || item.Exp > now {
			filtered = append(filtered, item)
		}
	}
	ds.RevokedSessions = filtered
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

func findCredentialByEmail(users []userRecord, email string) (credential, bool, error) {
	for _, rec := range users {
		if strings.EqualFold(strings.TrimSpace(rec.Email), strings.TrimSpace(email)) {
			return credential{User: toUser(rec), PasswordHash: rec.PasswordHash, PasswordSalt: rec.PasswordSalt}, true, nil
		}
	}
	return credential{}, false, nil
}

func indexOfUser(users []userRecord, userID string) int {
	for i := range users {
		if users[i].ID == userID {
			return i
		}
	}
	return -1
}

func toUser(rec userRecord) User {
	return User{
		ID:        rec.ID,
		Email:     rec.Email,
		Role:      rec.Role,
		Status:    rec.Status,
		CreatedAt: rec.CreatedAt,
		UpdatedAt: rec.UpdatedAt,
	}
}
