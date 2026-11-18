package main

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed console.html assets/*
var files embed.FS

var pageTemplate = template.Must(template.ParseFS(files, "console.html"))
var validName = regexp.MustCompile(`^[\p{L}\p{N}_.-]{1,40}$`)

const schema = `
CREATE TABLE IF NOT EXISTS users (
 id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, admin INTEGER NOT NULL DEFAULT 0,
 salt BLOB, password BLOB, invite BLOB UNIQUE, invite_until INTEGER
);
CREATE TABLE IF NOT EXISTS sessions (
 digest BLOB PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 expires INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS session_expiry ON sessions(expires);
CREATE TABLE IF NOT EXISTS api_keys (
 id INTEGER PRIMARY KEY, owner INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 label TEXT NOT NULL, digest BLOB NOT NULL UNIQUE, prefix TEXT NOT NULL,
 created INTEGER NOT NULL, revoked INTEGER, calls INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS key_owner ON api_keys(owner,id);
CREATE TABLE IF NOT EXISTS external_users (
 user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE
);
`

type account struct {
	ID    int64
	Name  string
	Admin bool
}
type keyRow struct {
	ID, Calls                     int64
	Label, Owner, Prefix, Created string
	Revoked                       bool
}
type page struct {
	AccessNote                                                    string
	View, Error, Secret, SecretTitle, InviteURL, BaseURL, Message string
	User                                                          account
	Keys                                                          []keyRow
	Mine                                                          bool
}
type attemptWindow struct {
	count int
	until time.Time
}
type console struct {
	publicFile   string
	externalOnly bool
	db           *sql.DB
	proxy        *httputil.ReverseProxy
	authMu       sync.Mutex
	attempts     map[string]attemptWindow
}
type apiKeyContext struct{}

func newToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
func digest(s string) []byte {
	d := sha256.Sum256([]byte(s))
	return d[:]
}
func passwordHash(password string, salt []byte) []byte {
	// All callers supply a 16-byte salt. PBKDF2 parameters stay fixed for this schema.
	d, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	if err != nil {
		panic(err)
	}
	return d
}
func newSalt() []byte {
	salt := make([]byte, 16)
	rand.Read(salt)
	return salt
}
func openDB(path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: abs}).String() + "?_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
func main() {
	addr := flag.String("listen", "127.0.0.1:18080", "listen address")
	dbFile := flag.String("db", "keys.sqlite3", "SQLite file")
	cert := flag.String("tls-cert", "", "certificate chain for public HTTPS")
	privateKey := flag.String("tls-key", "", "private key for public HTTPS")
	publicFile := flag.String("public-origin-file", "", "file containing the public HTTPS origin and an optional status line")
	externalOnly := flag.Bool("external-only", false, "accept only accounts invited for external access")
	flag.Parse()
	syscall.Umask(0077)
	db, err := openDB(*dbFile)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if flag.NArg() > 0 {
		if flag.NArg() != 1 || flag.Arg(0) != "init-admin" {
			log.Fatal("unexpected argument")
		}
		raw, err := io.ReadAll(io.LimitReader(os.Stdin, 1024))
		if err != nil {
			log.Fatal(err)
		}
		pass := strings.TrimRight(string(raw), "\r\n")
		if len(pass) < 12 || len(pass) > 256 {
			log.Fatal("admin password must be 12 to 256 bytes")
		}
		salt := newSalt()
		_, err = db.Exec("INSERT INTO users(name,admin,salt,password) VALUES('admin',1,?,?)", salt, passwordHash(pass, salt))
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		log.Fatal(err)
	}
	if (*cert == "") != (*privateKey == "") {
		log.Fatal("provide both TLS files")
	}
	if *cert == "" && !net.ParseIP(host).IsLoopback() {
		log.Fatal("public listeners require TLS")
	}
	upstream, _ := url.Parse("http://127.0.0.1:18000")
	app := newConsole(db, upstream)
	app.publicFile, app.externalOnly = *publicFile, *externalOnly
	srv := &http.Server{Addr: *addr, Handler: app.routes(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 64 << 10}
	stop, done := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer done()
	go func() {
		<-stop.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()
	log.Printf("listening on %s", *addr)
	if *cert != "" {
		err = srv.ListenAndServeTLS(*cert, *privateKey)
	} else {
		err = srv.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
func newConsole(db *sql.DB, upstream *url.URL) *console {
	c := &console{db: db, attempts: make(map[string]attemptWindow)}
	c.proxy = &httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.SetURL(upstream)
			p.Out.Header.Del("Authorization")
			p.Out.Header.Del("Cookie")
			p.Out.Header.Del("X-Api-Key")
			p.Out.Header.Del("Forwarded")
		},
		FlushInterval: -1,
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("Set-Cookie")
			if resp.Request.Method == http.MethodPost && resp.StatusCode >= 200 && resp.StatusCode < 300 {
				id := resp.Request.Context().Value(apiKeyContext{})
				if _, err := db.Exec("UPDATE api_keys SET calls=calls+1 WHERE id=?", id); err != nil {
					log.Printf("record call: %v", err)
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if r.Context().Err() == nil {
				apiError(w, 502, "GLM 服务暂时无法访问")
			}
		},
	}
	return c
}
func (c *console) routes() http.Handler {
	forms := http.NewCrossOriginProtection()
	m := http.NewServeMux()
	m.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ui", http.StatusSeeOther) })
	m.Handle("GET /assets/", http.FileServerFS(files))
	m.HandleFunc("GET /ui", c.dashboard)
	m.HandleFunc("GET /ui/login", func(w http.ResponseWriter, r *http.Request) { render(w, 200, page{View: "login"}) })
	m.HandleFunc("POST /ui/login", c.login)
	m.HandleFunc("POST /ui/logout", c.logout)
	m.HandleFunc("GET /ui/activate", func(w http.ResponseWriter, r *http.Request) { render(w, 200, page{View: "activate"}) })
	m.HandleFunc("POST /ui/activate", c.activate)
	m.HandleFunc("POST /ui/keys/create", c.createKey)
	m.HandleFunc("POST /ui/keys/revoke", c.revoke)
	m.HandleFunc("POST /ui/users/invite", c.invite)
	for _, route := range []string{"GET /v1/models", "POST /v1/chat/completions", "POST /v1/completions"} {
		m.HandleFunc(route, c.forward)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		if strings.HasPrefix(r.URL.Path, "/ui") {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
			if r.Method == http.MethodPost {
				hasSource := r.Header.Get("Sec-Fetch-Site") != "" || r.Header.Get("Origin") != ""
				if !hasSource || forms.Check(r) != nil {
					http.Error(w, "来源校验失败", 403)
					return
				}
				r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
				if err := r.ParseForm(); err != nil {
					http.Error(w, "表单无效", 400)
					return
				}
			}
		}
		m.ServeHTTP(w, r)
	})
}
func previewOrigin(r *http.Request) string {
	if r.TLS != nil {
		return "https://" + r.Host
	}
	return "http://" + r.Host
}
func (c *console) publicOrigin() (string, string) {
	if c.publicFile == "" {
		return "", ""
	}
	raw, err := os.ReadFile(c.publicFile)
	if err != nil {
		return "", ""
	}
	address, note, _ := strings.Cut(strings.TrimSpace(string(raw)), "\n")
	u, err := url.Parse(strings.TrimSpace(address))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", ""
	}
	h := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	ip := net.ParseIP(h)
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || ip.IsLoopback() || ip.IsUnspecified() {
		return "", ""
	}
	return "https://" + u.Host, strings.TrimSpace(note)
}
func render(w http.ResponseWriter, status int, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := pageTemplate.ExecuteTemplate(w, "console.html", p); err != nil {
		log.Printf("render: %v", err)
	}
}
func internal(w http.ResponseWriter, err error) {
	log.Printf("console: %v", err)
	http.Error(w, "服务暂时不可用", http.StatusServiceUnavailable)
}
func (c *console) user(w http.ResponseWriter, r *http.Request) (account, bool) {
	var a account
	cookie, err := r.Cookie("glm_console_session")
	if err == nil {
		err = c.db.QueryRow(`SELECT u.id,u.name,u.admin FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.digest=? AND s.expires>? AND (NOT ? OR (u.admin=0 AND u.id IN (SELECT user_id FROM external_users)))`, digest(cookie.Value), time.Now().Unix(), c.externalOnly).Scan(&a.ID, &a.Name, &a.Admin)
		if err == nil {
			return a, true
		}
		if !errors.Is(err, sql.ErrNoRows) {
			internal(w, err)
			return a, false
		}
	}
	http.Redirect(w, r, "/ui/login", http.StatusSeeOther)
	return a, false
}
func (c *console) sessionCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: "glm_console_session", Value: value, Path: "/ui", MaxAge: maxAge, Secure: r.TLS != nil || c.externalOnly, HttpOnly: true, SameSite: http.SameSiteStrictMode}
}
func (c *console) allowLogin(r *http.Request) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	c.authMu.Lock()
	defer c.authMu.Unlock()
	now := time.Now()
	for ip, b := range c.attempts {
		if now.After(b.until) {
			delete(c.attempts, ip)
		}
	}
	win := c.attempts[host]
	if win.count >= 30 {
		return false
	}
	if win.count == 0 {
		win.until = now.Add(time.Minute)
	}
	win.count++
	c.attempts[host] = win
	return true
}
func (c *console) login(w http.ResponseWriter, r *http.Request) {
	if !c.allowLogin(r) {
		render(w, 429, page{View: "login", Error: "登录过于频繁，请稍后再试"})
		return
	}
	name := strings.ToLower(strings.TrimSpace(r.PostForm.Get("username")))
	pass := r.PostForm.Get("password")
	var id int64
	var salt, stored []byte
	err := c.db.QueryRow("SELECT id,salt,password FROM users WHERE name=? AND password IS NOT NULL AND (NOT ? OR (admin=0 AND id IN (SELECT user_id FROM external_users)))", name, c.externalOnly).Scan(&id, &salt, &stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		internal(w, err)
		return
	}
	if len(salt) == 0 {
		salt = make([]byte, 16)
	}
	if len(pass) > 256 || subtle.ConstantTimeCompare(passwordHash(pass, salt), stored) != 1 {
		render(w, 401, page{View: "login", Error: "账号或密码有误"})
		return
	}
	token := newToken()
	now := time.Now()
	tx, err := c.db.Begin()
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM sessions WHERE expires<=?", now.Unix()); err == nil {
		_, err = tx.Exec("INSERT INTO sessions(digest,user_id,expires) VALUES(?,?,?)", digest(token), id, now.Add(12*time.Hour).Unix())
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		internal(w, err)
		return
	}
	http.SetCookie(w, c.sessionCookie(r, token, 12*3600))
	http.Redirect(w, r, "/ui", http.StatusSeeOther)
}
func (c *console) logout(w http.ResponseWriter, r *http.Request) {
	if ck, err := r.Cookie("glm_console_session"); err == nil {
		if _, err = c.db.Exec("DELETE FROM sessions WHERE digest=?", digest(ck.Value)); err != nil {
			internal(w, err)
			return
		}
	}
	http.SetCookie(w, c.sessionCookie(r, "", -1))
	http.Redirect(w, r, "/ui/login", http.StatusSeeOther)
}
func (c *console) invite(w http.ResponseWriter, r *http.Request) {
	a, ok := c.user(w, r)
	if !ok {
		return
	}
	if !a.Admin {
		http.Error(w, "需要管理员权限", 403)
		return
	}
	name := strings.ToLower(strings.TrimSpace(r.PostForm.Get("username")))
	if !validName.MatchString(name) {
		render(w, 400, page{View: "notice", Error: "账号需为 1 至 40 个字母、汉字、数字、点、横线或下划线"})
		return
	}
	token := newToken()
	public, note := c.publicOrigin()
	tx, err := c.db.Begin()
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO users(name,invite,invite_until) VALUES(?,?,?)
	ON CONFLICT(name) DO UPDATE SET invite=excluded.invite,invite_until=excluded.invite_until WHERE users.password IS NULL`, name, digest(token), time.Now().Add(7*24*time.Hour).Unix())
	if err != nil {
		internal(w, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		render(w, 409, page{View: "notice", Error: "该账号已开通"})
		return
	}
	title, msg, address := "本机测试邀请", "当前链接供本机测试使用。", previewOrigin(r)
	if public != "" {
		_, err = tx.Exec("INSERT OR IGNORE INTO external_users(user_id) SELECT id FROM users WHERE name=? AND admin=0", name)
		if err != nil {
			internal(w, err)
			return
		}
		title, msg, address = "验收账号邀请", "请将链接私下发给 "+name+"，由对方设置登录密码。", public
		if note != "" {
			msg += " " + note
		}
	}
	if err := tx.Commit(); err != nil {
		internal(w, err)
		return
	}
	render(w, 200, page{View: "secret", SecretTitle: title, Message: msg, InviteURL: address + "/ui/activate#" + token, User: a})
}
func (c *console) activate(w http.ResponseWriter, r *http.Request) {
	pass, token := r.PostForm.Get("password"), r.PostForm.Get("token")
	if len(pass) < 12 || len(pass) > 256 {
		render(w, 400, page{View: "activate", Error: "密码需为 12 至 256 字节"})
		return
	}
	var name string
	err := c.db.QueryRow("SELECT name FROM users WHERE invite=? AND invite_until>? AND password IS NULL AND (NOT ? OR (admin=0 AND id IN (SELECT user_id FROM external_users)))", digest(token), time.Now().Unix(), c.externalOnly).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		render(w, 400, page{View: "notice", Error: "邀请已使用或过期，请向管理员申请新链接"})
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	salt := newSalt()
	res, err := c.db.Exec("UPDATE users SET salt=?,password=?,invite=NULL,invite_until=NULL WHERE invite=? AND invite_until>? AND password IS NULL", salt, passwordHash(pass, salt), digest(token), time.Now().Unix())
	if err != nil {
		internal(w, err)
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		render(w, 409, page{View: "notice", Error: "邀请已失效"})
		return
	}
	render(w, 200, page{View: "login", Message: "已开通账号 " + name + "，请登录。"})
}
func (c *console) dashboard(w http.ResponseWriter, r *http.Request) {
	a, ok := c.user(w, r)
	if !ok {
		return
	}
	p := page{View: "dashboard", User: a, Mine: !a.Admin || r.URL.Query().Get("scope") == "mine"}
	if public, note := c.publicOrigin(); public != "" {
		p.BaseURL = public + "/v1"
		p.AccessNote = note
	}
	q := `SELECT k.id,k.label,u.name,k.prefix,k.created,k.revoked IS NOT NULL,k.calls FROM api_keys k JOIN users u ON u.id=k.owner`
	var params []any
	if p.Mine {
		q += " WHERE k.owner=?"
		params = append(params, a.ID)
	}
	rows, err := c.db.Query(q+" ORDER BY k.id DESC", params...)
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var k keyRow
		var stamp int64
		if err := rows.Scan(&k.ID, &k.Label, &k.Owner, &k.Prefix, &stamp, &k.Revoked, &k.Calls); err != nil {
			internal(w, err)
			return
		}
		k.Created = time.Unix(stamp, 0).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04")
		p.Keys = append(p.Keys, k)
	}
	if err := rows.Err(); err != nil {
		internal(w, err)
		return
	}
	render(w, 200, p)
}
func (c *console) createKey(w http.ResponseWriter, r *http.Request) {
	a, ok := c.user(w, r)
	if !ok {
		return
	}
	label := strings.TrimSpace(r.PostForm.Get("label"))
	if label == "" || utf8.RuneCountInString(label) > 64 {
		render(w, 400, page{View: "notice", Error: "Key 名称需为 1 至 64 个字符"})
		return
	}
	token := "sk-glm-" + newToken()
	_, err := c.db.Exec("INSERT INTO api_keys(owner,label,digest,prefix,created) VALUES(?,?,?,?,?)", a.ID, label, digest(token), token[:15], time.Now().Unix())
	if err != nil {
		internal(w, err)
		return
	}
	render(w, 200, page{View: "secret", SecretTitle: "API Key 已创建", Secret: token, Message: "请复制并妥善保存。离开后只显示 Key 前缀。", User: a})
}
func (c *console) revoke(w http.ResponseWriter, r *http.Request) {
	a, ok := c.user(w, r)
	if !ok {
		return
	}
	res, err := c.db.Exec("UPDATE api_keys SET revoked=COALESCE(revoked,?) WHERE id=? AND (owner=? OR ?)", time.Now().Unix(), r.PostForm.Get("id"), a.ID, a.Admin)
	if err != nil {
		internal(w, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/ui", http.StatusSeeOther)
}
func apiError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	if status == 401 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="GLM"`)
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": msg, "type": http.StatusText(status)}})
}
func (c *console) forward(w http.ResponseWriter, r *http.Request) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || len(token) != 50 || !strings.HasPrefix(token, "sk-glm-") {
		apiError(w, 401, "API Key 无效或已停用")
		return
	}
	var id int64
	err := c.db.QueryRow("SELECT id FROM api_keys WHERE digest=? AND revoked IS NULL AND (NOT ? OR owner IN (SELECT user_id FROM external_users))", digest(token), c.externalOnly).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, 401, "API Key 无效或已停用")
		return
	}
	if err != nil {
		apiError(w, 503, "Key 校验暂时不可用")
		return
	}
	c.proxy.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), apiKeyContext{}, id)))
}
