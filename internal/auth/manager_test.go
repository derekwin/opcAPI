package auth

import "testing"

func TestParseKeys(t *testing.T) {
	keys := parseKeys(" sk-a , ,sk-b,sk-c ")
	if len(keys) != 3 {
		t.Fatalf("unexpected key count: %d", len(keys))
	}
}

func TestHashAPIKey(t *testing.T) {
	h := HashAPIKey("sk-test")
	if len(h) != 64 {
		t.Fatalf("unexpected hash length: %d", len(h))
	}
}

func TestAuthenticateDisabled(t *testing.T) {
	m := NewStaticManager(false, nil)
	key, err := m.Authenticate("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "anonymous" {
		t.Fatalf("unexpected key: %s", key)
	}
}

func TestAuthenticateEnabled(t *testing.T) {
	m := NewStaticManager(true, []string{"sk-test"})

	if _, err := m.Authenticate(""); err != ErrMissingAuthorization {
		t.Fatalf("expected ErrMissingAuthorization, got %v", err)
	}
	if _, err := m.Authenticate("invalid"); err != ErrInvalidAuthorization {
		t.Fatalf("expected ErrInvalidAuthorization, got %v", err)
	}
	if _, err := m.Authenticate("Bearer sk-wrong"); err != ErrInvalidAPIKey {
		t.Fatalf("expected ErrInvalidAPIKey, got %v", err)
	}

	hash, err := m.Authenticate("Bearer sk-test")
	if err != nil {
		t.Fatalf("unexpected auth error: %v", err)
	}
	if hash != HashAPIKey("sk-test") {
		t.Fatalf("unexpected hash: %s", hash)
	}
}

func TestAuthenticateWithDynamicKeys(t *testing.T) {
	m := NewStaticManager(true, nil)
	m.SetDynamicKeyHashes([]string{HashAPIKey("sk-dynamic")})

	hash, err := m.Authenticate("Bearer sk-dynamic")
	if err != nil {
		t.Fatalf("unexpected auth error: %v", err)
	}
	if hash != HashAPIKey("sk-dynamic") {
		t.Fatalf("unexpected hash: %s", hash)
	}
}

func TestNewStaticManagerSupportsSha256Prefix(t *testing.T) {
	hash := HashAPIKey("sk-hashed")
	m := NewStaticManager(true, []string{"sha256:" + hash})

	if _, err := m.Authenticate("Bearer sk-hashed"); err != nil {
		t.Fatalf("expected hashed key to authenticate, got %v", err)
	}
}
