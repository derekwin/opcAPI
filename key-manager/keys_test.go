package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestKeyLifecycle(t *testing.T) {
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("forwarded credential")
		}
		if r.URL.Path == "/v1/tokenize" {
			fmt.Fprint(w, `{"count":3}`)
			return
		}
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"glm","max_model_len":262144}]}`)
			return
		}
		calls.Add(1)
		var c struct{ Stream bool }
		json.NewDecoder(r.Body).Decode(&c)
		if !c.Stream {
			fmt.Fprint(w, `{"usage":{"prompt_tokens":3,"total_tokens":4}}`)
			return
		}
		close(entered)
		<-release
		fmt.Fprintln(w, "data: {\"usage\":{\"total_tokens\":7}}\n\ndata: [DONE]")
	}))
	defer up.Close()
	g := newGateway()
	g.file = filepath.Join(t.TempDir(), "keys")
	g.upstream = up.URL
	admin := func(v url.Values) string {
		r := httptest.NewRequest("POST", "http://localhost/", strings.NewReader(v.Encode()))
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		g.admin(w, r)
		if w.Code >= 400 {
			t.Fatal(w.Code, w.Body.String())
		}
		if w.Code == 303 {
			page := httptest.NewRecorder()
			g.admin(page, httptest.NewRequest("GET", "http://localhost"+w.Header().Get("Location"), nil))
			return page.Body.String()
		}
		return w.Body.String()
	}
	create := func() string {
		body := admin(url.Values{"action": {"create"}, "label": {"trial"}, "limit": {"37"}, "rpm": {"30"}, "parallel": {"2"}, "days": {"7"}})
		key := regexp.MustCompile("sk-[0-9a-f]{64}").FindString(body)
		if key == "" {
			t.Fatal("missing key")
		}
		return key
	}
	a, b := create(), create()
	call := func(g *gateway, key, path string) int {
		method := "GET"
		if path != "/v1/models" {
			method = "POST"
		}
		r := httptest.NewRequest(method, path, strings.NewReader(`{"model":"glm","messages":[{"role":"user","content":"hi"}],"stream":true}`))
		r.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		g.public(w, r)
		return w.Code
	}
	done := make(chan int, 1)
	go func() { done <- call(g, a, "/v1/chat/completions") }()
	<-entered
	blocked := call(g, a, "/v1/chat/completions")
	close(release)
	if blocked != 429 || <-done != 200 || calls.Load() != 1 {
		t.Fatal("reservation failed")
	}
	fresh := newGateway()
	fresh.file = g.file
	fresh.upstream = g.upstream
	page := httptest.NewRecorder()
	fresh.admin(page, httptest.NewRequest("GET", "http://localhost/", nil))
	if !strings.Contains(page.Body.String(), a) || !strings.Contains(page.Body.String(), b) {
		t.Fatal("issued keys missing after reload")
	}
	if err := fresh.update(func(m map[string]*keyRecord) error {
		if m[hashKey(a)].Used != 7 {
			t.Error("incorrect settlement")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"glm","messages":[{"role":"user","content":"hi"}]}`))
	r.Header.Set("Authorization", "Bearer "+b)
	w := httptest.NewRecorder()
	fresh.public(w, r)
	if w.Code != 200 || calls.Load() != 2 {
		t.Fatal("expected one upstream call per accepted chat", w.Code, calls.Load())
	}
	admin(url.Values{"action": {"revoke"}, "id": {hashKey(a)[:16]}})
	if call(fresh, a, "/v1/models") != 401 || call(fresh, b, "/v1/models") != 200 {
		t.Fatal("revoke isolation failed")
	}
}

func TestDeletionDuringGeneration(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tokenize" {
			fmt.Fprint(w, `{"count":3}`)
			return
		}
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"glm","max_model_len":262144}]}`)
			return
		}
		close(started)
		<-finish
		fmt.Fprint(w, `{"usage":{"total_tokens":9}}`)
	}))
	defer up.Close()
	g := newGateway()
	g.file = filepath.Join(t.TempDir(), "keys")
	g.upstream = up.URL
	a, b := "sk-a", "sk-b"
	err := g.update(func(m map[string]*keyRecord) error {
		for _, s := range []string{a, b} {
			m[hashKey(s)] = &keyRecord{ID: s, Label: s, Expires: time.Now().Add(time.Hour), Limit: 1000, RPM: 10, Concurrent: 1}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"glm","messages":[{"role":"user","content":"hi"}]}`))
		r.Header.Set("Authorization", "Bearer "+a)
		g.public(httptest.NewRecorder(), r)
	}()
	<-started
	r := httptest.NewRequest("POST", "http://localhost/", strings.NewReader("action=delete&id=sk-a"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://localhost")
	w := httptest.NewRecorder()
	g.admin(w, r)
	close(finish)
	<-done
	if w.Code != 303 {
		t.Fatal(w.Code)
	}
	r = httptest.NewRequest("GET", "/v1/models", nil)
	r.Header.Set("Authorization", "Bearer "+a)
	w = httptest.NewRecorder()
	g.public(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	if err := g.withRecords(false, func(m map[string]*keyRecord) error {
		if len(m) != 1 || m[hashKey(b)] == nil {
			t.Error("wrong remaining keys")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestUnlimitedQuota(t *testing.T) {
	g := newGateway()
	g.file = filepath.Join(t.TempDir(), "keys.json")
	admin := func(values url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://localhost/", strings.NewReader(values.Encode()))
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		g.admin(w, r)
		return w
	}
	w := admin(url.Values{"action": {"create"}, "label": {"unlimited"}, "unlimited": {"1"}, "days": {"1"}, "rpm": {"2"}, "parallel": {"1"}})
	if w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	page := httptest.NewRecorder()
	g.admin(page, httptest.NewRequest("GET", "http://localhost/", nil))
	key := regexp.MustCompile("sk-[0-9a-f]{64}").FindString(page.Body.String())
	if key == "" || !strings.Contains(page.Body.String(), `data-unlimited="true"`) {
		t.Fatal("missing unlimited key")
	}
	hash := hashKey(key)
	change := func(action string) int {
		return admin(url.Values{"id": {hash[:16]}, "action": {action}, "extra": {"5"}}).Code
	}
	entered, release := make(chan struct{}), make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Error("unexpected quota lookup", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		close(entered)
		<-release
		fmt.Fprint(w, `{"usage":{"total_tokens":31}}`)
	}))
	defer up.Close()
	g.upstream = up.URL
	call := func(path string) *httptest.ResponseRecorder {
		method := "GET"
		if path == "/v1/chat/completions" {
			method = "POST"
		}
		r := httptest.NewRequest(method, path, strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`))
		r.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		g.public(w, r)
		return w
	}
	done := make(chan int, 1)
	go func() { done <- call("/v1/chat/completions").Code }()
	select {
	case <-entered:
	case code := <-done:
		t.Fatalf("generation did not start: %d", code)
	}
	busy := change("limited")
	parallel := call("/v1/models")
	close(release)
	if <-done != 200 || busy != 409 || parallel.Code != 429 || !strings.Contains(parallel.Body.String(), "concurrency_limit_exceeded") {
		t.Fatal("unlimited admission or in-flight transition")
	}
	if change("limited") != 303 || change("limited") != 303 {
		t.Fatal("restore limit")
	}
	if err := g.withRecords(false, func(m map[string]*keyRecord) error {
		k := m[hash]
		if k.Unlimited || k.Used != 31 || k.Remaining() != 5 {
			t.Errorf("used %d, remaining %d", k.Used, k.Remaining())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if change("unlimited") != 303 {
		t.Fatal("enable unlimited")
	}
	if err := g.update(func(m map[string]*keyRecord) error {
		k := m[hash]
		k.Used = k.Limit
		if k.Status() != "可用" {
			t.Error("unlimited key marked exhausted")
		}
		g.windows[hash].minute = time.Now().Unix() / 60
		g.windows[hash].requests = k.RPM
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if w := call("/v1/models"); w.Code != 429 || !strings.Contains(w.Body.String(), "rate_limit_exceeded") {
		t.Fatal("RPM not enforced")
	}
	if change("revoke") != 303 || call("/v1/models").Code != 401 {
		t.Fatal("revoked unlimited key accepted")
	}
}
