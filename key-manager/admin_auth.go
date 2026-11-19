package main

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"errors"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const sessionTTL = 12 * time.Hour
const iterations = 600000

type adminAccount struct {
	Username string
	Salt     []byte
	Digest   []byte
}
type adminAuth struct {
	mu       sync.Mutex
	file     string
	account  *adminAccount
	sessions map[[32]byte]time.Time
	failures int
	retryAt  time.Time
	next     http.Handler
}

//go:embed auth.html
var authHTML string
var authPage = template.Must(template.New("auth").Parse(authHTML))

func authenticatedAdmin(file string, next http.Handler) (http.Handler, error) {
	a := &adminAuth{file: file, sessions: make(map[[32]byte]time.Time), next: next}
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	var account adminAccount
	if err = json.Unmarshal(data, &account); err != nil {
		return nil, err
	}
	if account.Username == "" || len(account.Salt) != 16 || len(account.Digest) != sha256.Size {
		return nil, errors.New("invalid administrator credential file")
	}
	a.account = &account
	return a, nil
}

func passwordDigest(password string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, salt, iterations, sha256.Size)
}
func (a *adminAccount) matches(password string) bool {
	if len(password) == 0 || len(password) > 512 {
		return false
	}
	digest, err := passwordDigest(password, a.Salt)
	return err == nil && subtle.ConstantTimeCompare(digest, a.Digest) == 1
}

// Replace the on-disk credential before changing the active password or sessions.
func (a *adminAuth) save(username, password string) error {
	account := adminAccount{Username: username, Salt: make([]byte, 16)}
	if _, err := rand.Read(account.Salt); err != nil {
		return err
	}
	digest, err := passwordDigest(password, account.Salt)
	if err != nil {
		return err
	}
	account.Digest = digest
	f, err := os.CreateTemp(filepath.Dir(a.file), ".admin-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(account)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), a.file); err != nil {
		return err
	}
	a.account = &account
	clear(a.sessions)
	a.failures = 0
	return nil
}
func renderAuth(w http.ResponseWriter, status int, path, message, username string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = authPage.Execute(w, struct{ Path, Message, Username string }{path, message, username})
}
func sessionCookieName(r *http.Request) string {
	// An HTTP response cannot replace a Secure cookie left by an HTTPS login.
	if r.TLS == nil {
		return "key_manager_http_session"
	}
	return "key_manager_session"
}
func cookieHash(r *http.Request) [32]byte {
	c, err := r.Cookie(sessionCookieName(r))
	if err != nil {
		return [32]byte{}
	}
	return sha256.Sum256([]byte(c.Value))
}
func setSessionCookie(w http.ResponseWriter, r *http.Request, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName(r), Value: value, Path: "/", MaxAge: age, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
}
func (a *adminAuth) login(w http.ResponseWriter, r *http.Request) {
	token := rand.Text()
	now := time.Now()
	for key, expiry := range a.sessions {
		if !expiry.After(now) {
			delete(a.sessions, key)
		}
	}
	delete(a.sessions, cookieHash(r))
	a.sessions[sha256.Sum256([]byte(token))] = now.Add(sessionTTL)
	setSessionCookie(w, r, token, int(sessionTTL.Seconds()))
	a.failures = 0
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Public administration and SSH-forwarded administration share the same
// handler. The listener address distinguishes TCP from the private Unix socket.
func adminOverTCP(r *http.Request) bool {
	local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	return local != nil && local.Network() == "tcp"
}

func (a *adminAuth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	host := (&url.URL{Host: r.Host}).Hostname()
	if host != "localhost" && host != "127.0.0.1" && host != "::1" && !(adminOverTCP(r) && net.ParseIP(host) != nil) {
		http.Error(w, "管理入口地址无效", 403)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	if r.Method == http.MethodPost {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if r.Header.Get("Origin") != scheme+"://"+r.Host {
			http.Error(w, "来源无效", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "表单无效", 400)
			return
		}
	}
	if r.URL.Path == "/admin.css" && r.Method == "GET" {
		a.next.ServeHTTP(w, r)
		return
	}
	if a.authorize(w, r) {
		a.next.ServeHTTP(w, r)
	}
}

// Authentication state transitions are serialized. Key management runs outside this lock.
func (a *adminAuth) authorize(w http.ResponseWriter, r *http.Request) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	path := r.URL.Path
	if a.account == nil && path != "/register" {
		http.Redirect(w, r, "/register", 303)
		return false
	}
	if a.account != nil && path == "/register" {
		http.Redirect(w, r, "/login", 303)
		return false
	}
	signedIn := a.sessions[cookieHash(r)].After(time.Now())
	if path != "/login" && path != "/register" && !signedIn {
		http.Redirect(w, r, "/login", 303)
		return false
	}
	if path == "/logout" {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return false
		}
		delete(a.sessions, cookieHash(r))
		setSessionCookie(w, r, "", -1)
		http.Redirect(w, r, "/login", 303)
		return false
	}
	if path != "/login" && path != "/register" && path != "/password" {
		return true
	}
	if r.Method == "GET" {
		if path == "/login" && signedIn {
			http.Redirect(w, r, "/", 303)
			return false
		}
		username := ""
		if path == "/password" {
			username = a.account.Username
		}
		message := ""
		if path == "/login" && r.URL.Query().Get("changed") == "1" {
			message = "密码已更新，请用新密码登录。"
		}
		renderAuth(w, 200, path, message, username)
		return false
	}
	now := time.Now()
	if now.After(a.retryAt) {
		a.failures = 0
		a.retryAt = now.Add(time.Minute)
	}
	if a.failures >= 10 {
		renderAuth(w, 429, path, "尝试次数过多，请稍后再试。", "")
		return false
	}
	username := strings.TrimSpace(r.PostForm.Get("username"))
	password := r.PostForm.Get("password")
	var credentialError string
	switch path {
	case "/login":
		if !a.account.matches(password) || username != a.account.Username {
			credentialError = "用户名或密码有误。"
		}
	case "/password":
		username = a.account.Username
		if !a.account.matches(r.PostForm.Get("current")) {
			credentialError = "当前密码有误。"
		}
	}
	if credentialError != "" {
		a.failures++
		renderAuth(w, 401, path, credentialError, username)
		return false
	}
	if path == "/login" {
		a.login(w, r)
		return false
	}
	if username == "" || utf8.RuneCountInString(username) > 64 {
		renderAuth(w, 400, path, "用户名需为 1 至 64 个字符。", username)
		return false
	}
	if password == "" || len(password) > 512 {
		renderAuth(w, 400, path, "密码需为 1 至 512 字节。", username)
		return false
	}
	if password != r.PostForm.Get("confirm") {
		renderAuth(w, 400, path, "两次输入的密码不一致。", username)
		return false
	}
	if path == "/password" && password == r.PostForm.Get("current") {
		renderAuth(w, 400, path, "请输入与当前密码不同的新密码。", username)
		return false
	}
	if err := a.save(username, password); err != nil {
		http.Error(w, "账号保存失败，请重试。", 503)
		return false
	}
	if path == "/password" {
		setSessionCookie(w, r, "", -1)
		http.Redirect(w, r, "/login?changed=1", 303)
		return false
	}
	a.login(w, r)
	return false
}
