package main

import (
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type keyRecord struct {
	ID, Label       string
	Secret          string `json:"secret,omitempty"`
	Expires         time.Time
	Limit, Used     int64
	Unlimited       bool `json:"Unlimited,omitempty"`
	RPM, Concurrent int
	LegacyOutput    int `json:"Output,omitempty"` // Preserve old records when writing the key store.
	Revoked         bool
}
type rateWindow struct {
	minute, held     int64
	requests, active int
}
type gateway struct {
	mu             sync.Mutex
	file, upstream string
	windows        map[string]*rateWindow
	maxBody        int64
}

func newGateway() *gateway {
	return &gateway{maxBody: defaultMaxBody, file: "keys.json", upstream: "http://127.0.0.1:18000", windows: make(map[string]*rateWindow)}
}

// Serialize read-modify-write operations in the gateway process.
func (g *gateway) update(fn func(map[string]*keyRecord) error) error {
	return g.withRecords(true, fn)
}

func (g *gateway) withRecords(write bool, fn func(map[string]*keyRecord) error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	records := map[string]*keyRecord{}
	data, err := os.ReadFile(g.file)
	if err == nil {
		if err = json.Unmarshal(data, &records); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = fn(records); err != nil {
		return err
	}
	if !write {
		return nil
	}
	if err = os.MkdirAll(filepath.Dir(g.file), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(g.file), ".keys-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(records)
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
	return os.Rename(f.Name(), g.file)
}

func (k keyRecord) Remaining() int64 { return max(0, k.Limit-k.Used) }

//go:embed admin.html
var adminHTML string

//go:embed admin.css
var adminCSS string

//go:embed admin.js
var adminJS string

func formatCount(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

//go:embed guide.html
var guideHTML string

var keyPage = template.Must(template.New("keys").Funcs(template.FuncMap{"count": formatCount}).Parse(adminHTML + guideHTML))

func (k keyRecord) Preview() string {
	if len(k.Secret) < 14 {
		return k.ID
	}
	return k.Secret[:7] + "…" + k.Secret[len(k.Secret)-4:]
}
func (k keyRecord) ExpiryDate() string {
	return k.Expires.In(time.FixedZone("UTC+8", 8*3600)).Format("2006-01-02")
}

func (k keyRecord) Status() string {
	if k.Revoked {
		return "已停用"
	}
	if !time.Now().Before(k.Expires) {
		return "已到期"
	}
	if !k.Unlimited && k.Remaining() == 0 {
		return "额度已用完"
	}
	return "可用"
}

func (g *gateway) admin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	switch r.URL.Path {
	case "/admin.css", "/admin.js":
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		if r.URL.Path == "/admin.css" {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			io.WriteString(w, adminCSS)
		} else {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			io.WriteString(w, adminJS)
		}
		return
	case "/":
	default:
		http.NotFound(w, r)
		return
	}
	var createdID string
	var list []keyRecord
	if r.Method != "GET" && r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	if r.Method == "POST" {
		u, e := url.Parse(r.Header.Get("Origin"))
		if e != nil || u.Host != r.Host || u.Host == "" {
			http.Error(w, "来源无效", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if r.ParseForm() != nil {
			w.WriteHeader(400)
			return
		}
	}
	bad := errors.New("参数无效")
	busy := errors.New("该 Key 还有进行中的请求，请在请求结束后设置剩余额度")
	err := g.withRecords(r.Method == http.MethodPost, func(m map[string]*keyRecord) error {
		if r.Method == "POST" {
			switch r.PostForm.Get("action") {
			case "create":
				unlimited := r.PostForm.Get("unlimited") == "1"
				fields := []string{"days", "rpm", "parallel"}
				if !unlimited {
					fields = append(fields, "limit")
				}
				vals := map[string]int{}
				for _, k := range fields {
					n, e := strconv.Atoi(r.PostForm.Get(k))
					if e != nil || n < 1 || n > 1e9 {
						return bad
					}
					vals[k] = n
				}
				label := strings.TrimSpace(r.PostForm.Get("label"))
				if label == "" || len([]rune(label)) > 100 || vals["days"] > 3650 {
					return bad
				}
				b := make([]byte, 32)
				if _, e := rand.Read(b); e != nil {
					return e
				}
				secret := "sk-" + hex.EncodeToString(b)
				h := hashKey(secret)
				createdID = h[:16]
				m[h] = &keyRecord{ID: h[:16], Label: label, Secret: secret, Expires: time.Now().UTC().AddDate(0, 0, vals["days"]), Limit: int64(vals["limit"]), Unlimited: unlimited, RPM: vals["rpm"], Concurrent: vals["parallel"]}
			case "revoke", "add", "delete", "unlimited", "limited":
				var hash string
				var k *keyRecord
				for digest, v := range m {
					if v.ID == r.PostForm.Get("id") {
						k = v
						hash = digest
						break
					}
				}
				if k == nil {
					return bad
				}
				switch r.PostForm.Get("action") {
				case "delete":
					delete(m, hash)
					delete(g.windows, hash)
				case "revoke":
					k.Revoked = true
				case "unlimited":
					k.Unlimited = true
				case "limited":
					if !k.Unlimited {
						break
					}
					n, err := strconv.ParseInt(r.PostForm.Get("extra"), 10, 32)
					if err != nil || n < 1 {
						return bad
					}
					if slot := g.windows[hash]; slot != nil && slot.active > 0 {
						return busy
					}
					k.Limit = k.Used + n
					k.Unlimited = false
				case "add":
					n, err := strconv.ParseInt(r.PostForm.Get("extra"), 10, 32)
					if err != nil || n < 1 || k.Unlimited {
						return bad
					}
					k.Limit += n
				}
			default:
				return bad
			}
		}
		for digest, v := range m {
			row := *v
			if slot := g.windows[digest]; slot != nil {
				row.Used += slot.held
			}
			list = append(list, row)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, busy) {
			http.Error(w, busy.Error(), http.StatusConflict)
		} else if errors.Is(err, bad) {
			http.Error(w, bad.Error(), 400)
		} else {
			http.Error(w, "存储不可用", 503)
		}
		return
	}
	if r.Method == "POST" {
		dest := "/?done=" + url.QueryEscape(r.PostForm.Get("action"))
		if createdID != "" {
			dest += "&created=" + createdID
		}
		http.Redirect(w, r, dest, 303)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	sort.Slice(list, func(i, j int) bool {
		return list[i].Label < list[j].Label || list[i].Label == list[j].Label && list[i].ID < list[j].ID
	})
	var created *keyRecord
	for i := range list {
		if list[i].ID == r.URL.Query().Get("created") {
			created = &list[i]
			break
		}
	}
	notices := map[string]string{"unlimited": "已设为不限累计额度", "limited": "已启用累计额度限制", "add": "额度已追加", "revoke": "Key 已停用", "delete": "Key 已删除"}
	if err := keyPage.Execute(w, struct {
		Created *keyRecord
		Notice  string
		Records []keyRecord
	}{created, notices[r.URL.Query().Get("done")], list}); err != nil {
		log.Printf("render key list: %v", err)
	}
}
func hashKey(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
func (g *gateway) public(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/guide" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
		if r.Method == http.MethodGet {
			if err := keyPage.ExecuteTemplate(w, "public-guide", nil); err != nil {
				log.Printf("render guide: %v", err)
			}
		}
		return
	}
	path := r.URL.Path
	modelID, detail := strings.CutPrefix(path, "/v1/models/")
	models := path == "/v1/models" || detail && modelID != "" && modelID != "." && modelID != ".." && !strings.Contains(modelID, "/")
	if !(models && r.Method == http.MethodGet || path == "/v1/chat/completions" && r.Method == http.MethodPost) {
		http.NotFound(w, r)
		return
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !strings.HasPrefix(parts[1], "sk-") {
		apiError(w, 401, "invalid_api_key", "", "A valid Bearer API key is required.")
		return
	}
	h := hashKey(parts[1])
	var slot *rateWindow
	var unlimited bool
	err := g.withRecords(false, func(m map[string]*keyRecord) error {
		k := m[h]
		if k == nil || k.Revoked || !time.Now().Before(k.Expires) {
			return errInvalidKey
		}
		slot = g.windows[h]
		if slot == nil {
			slot = &rateWindow{}
			g.windows[h] = slot
		}
		minute := time.Now().Unix() / 60
		if minute != slot.minute {
			slot.minute = minute
			slot.requests = 0
		}
		switch {
		case slot.requests >= k.RPM:
			return errRPM
		case slot.active >= k.Concurrent:
			return errParallel
		case !k.Unlimited && k.Used >= k.Limit:
			return errQuota
		}
		slot.requests++
		slot.active++
		unlimited = k.Unlimited
		return nil
	})
	if err != nil {
		writeAdmissionError(w, err)
		return
	}
	defer func() { g.mu.Lock(); slot.active--; g.mu.Unlock() }()
	if models {
		if detail {
			path = "/v1/models/" + url.PathEscape(modelID)
		}
		resp, err := g.call(r, path, nil)
		if err != nil {
			apiError(w, 502, "upstream_unavailable", "", "Model service unavailable.")
			return
		}
		defer resp.Body.Close()
		copyResponse(w, resp)
		return
	}
	c, ok := readChat(w, r, g.maxBody)
	if !ok {
		return
	}
	var held int64
	if !unlimited {
		held, ok = g.reserveQuota(w, r, c, h, slot)
		if !ok {
			return
		}
	}
	defer func() { g.mu.Lock(); slot.held -= held; g.mu.Unlock() }()
	resp, err := g.call(r, "/v1/chat/completions", c.fields)
	if err != nil {
		apiError(w, 502, "upstream_unavailable", "", "Model service unavailable.")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		copyResponse(w, resp)
		return
	}

	settle := func(total int64) error {
		if total <= 0 {
			return nil
		}
		return g.update(func(m map[string]*keyRecord) error {
			if k := m[h]; k != nil {
				k.Used += total
			}
			// Release under the same lock that books actual usage.
			slot.held -= held
			held = 0
			return nil
		})
	}
	if c.stream {
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		total := relayStream(w, resp.Body, c)
		if err := settle(total); err != nil {
			log.Printf("settle stream %s: %v", h[:16], err)
		}
		return
	}
	data, err := io.ReadAll(resp.Body)
	var result struct{ Usage *tokenUsage }
	if err != nil || json.Unmarshal(data, &result) != nil || result.Usage == nil || result.Usage.Total == nil || *result.Usage.Total < 0 {
		apiError(w, 502, "invalid_upstream_response", "", "Incomplete model response or missing usage.")
		return
	}
	if err := settle(*result.Usage.Total); err != nil {
		apiError(w, 503, "storage_unavailable", "", "Could not record token usage.")
		return
	}
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	_, _ = w.Write(data)
}

func (g *gateway) reserveQuota(w http.ResponseWriter, r *http.Request, c *chatRequest, h string, slot *rateWindow) (int64, bool) {
	prompt, ok := g.promptTokens(w, r, c)
	if !ok {
		return 0, false
	}

	// For an omitted limit, reserve up to the served model's context window.
	// Leave the request unchanged when the key can cover that generation.
	var requested int64
	if c.maxTokens != nil {
		requested = *c.maxTokens
	} else {
		contextLength, ok := g.modelContextLength(w, r, c)
		if !ok {
			return 0, false
		}
		requested = max(1, contextLength-prompt)
	}
	var held int64
	err := g.withRecords(false, func(m map[string]*keyRecord) error {
		k := m[h]
		if k == nil || k.Revoked || !time.Now().Before(k.Expires) {
			return errInvalidKey
		}
		if k.Unlimited {
			return nil
		}
		available := k.Limit - k.Used - slot.held
		if available < prompt || (requested > 0 && available-prompt < c.n) {
			return errQuota
		}
		perChoice := min(requested, (available-prompt)/c.n)
		if perChoice < requested {
			delete(c.fields, "max_completion_tokens")
			c.fields["max_tokens"] = json.RawMessage(strconv.FormatInt(perChoice, 10))
		}
		held = prompt + c.n*perChoice
		slot.held += held
		return nil
	})
	if err != nil {
		writeAdmissionError(w, err)
		return 0, false
	}
	return held, true
}

var (
	errInvalidKey = errors.New("The API key is invalid, expired or revoked.")
	errRPM        = errors.New("The key's requests per minute limit was reached.")
	errParallel   = errors.New("The key's concurrent request limit was reached.")
	errQuota      = errors.New("Insufficient available token quota for this request.")
)

func writeAdmissionError(w http.ResponseWriter, err error) {
	switch err {
	case errInvalidKey:
		apiError(w, 401, "invalid_api_key", "", err.Error())
	case errRPM:
		w.Header().Set("Retry-After", strconv.FormatInt(60-time.Now().Unix()%60, 10))
		apiError(w, 429, "rate_limit_exceeded", "", err.Error())
	case errParallel:
		apiError(w, 429, "concurrency_limit_exceeded", "", err.Error())
	case errQuota:
		apiError(w, 429, "insufficient_quota", "", err.Error())
	default:
		apiError(w, 503, "storage_unavailable", "", "Key store unavailable.")
	}
}
