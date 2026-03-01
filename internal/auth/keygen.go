package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

func GenerateAPIKey(prefix string) string {
	p := strings.TrimSpace(prefix)
	if p == "" {
		p = "sk-opcapi-"
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s%d", p, time.Now().UnixNano())
	}
	return p + hex.EncodeToString(buf)
}
