package user

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type SessionManager struct {
	secret []byte
	ttl    time.Duration
}

func NewSessionManager(secret string, ttl time.Duration) *SessionManager {
	if secret == "" {
		secret = "opcapi-dev-session-secret"
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	return &SessionManager{secret: []byte(secret), ttl: ttl}
}

func (s *SessionManager) Issue(userID, role string) (string, SessionClaims, error) {
	now := time.Now().UTC()
	claims := SessionClaims{
		UserID: userID,
		Role:   role,
		JTI:    newJTI(),
		Iat:    now.Unix(),
		Exp:    now.Add(s.ttl).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", SessionClaims{}, err
	}

	body := base64.RawURLEncoding.EncodeToString(payload)
	sig := s.sign(body)
	return body + "." + sig, claims, nil
}

func (s *SessionManager) Parse(token string) (SessionClaims, error) {
	token = strings.TrimSpace(token)
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return SessionClaims{}, ErrSessionTokenInvalid
	}

	body := parts[0]
	sig := parts[1]
	if !hmac.Equal([]byte(sig), []byte(s.sign(body))) {
		return SessionClaims{}, ErrSessionTokenInvalid
	}

	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return SessionClaims{}, ErrSessionTokenInvalid
	}

	var claims SessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return SessionClaims{}, ErrSessionTokenInvalid
	}
	if claims.UserID == "" || claims.Role == "" || claims.JTI == "" {
		return SessionClaims{}, ErrSessionTokenInvalid
	}

	if time.Now().UTC().Unix() >= claims.Exp {
		return SessionClaims{}, ErrSessionTokenExpired
	}
	return claims, nil
}

func (s *SessionManager) sign(body string) string {
	h := hmac.New(sha256.New, s.secret)
	_, _ = h.Write([]byte(body))
	return hex.EncodeToString(h.Sum(nil))
}

func newJTI() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("jti-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
