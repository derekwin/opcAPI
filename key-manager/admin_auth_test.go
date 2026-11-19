package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminAccountLifecycle(t *testing.T) {
	file := filepath.Join(t.TempDir(), "admin-account.json")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	h, err := authenticatedAdmin(file, next)
	if err != nil {
		t.Fatal(err)
	}
	call := func(path string, v url.Values, c *http.Cookie, origin string, code int, location string) *httptest.ResponseRecorder {
		t.Helper()
		method := "GET"
		if v != nil {
			method = "POST"
		}
		r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(v.Encode()))
		if v != nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", origin)
		}
		if c != nil {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != code || w.Header().Get("Location") != location {
			t.Fatalf("%s %s: got %d %q want %d %q", method, path, w.Code, w.Header().Get("Location"), code, location)
		}
		return w
	}
	const origin = "http://localhost"
	const old = "a"
	const updated = "b!"
	signup := url.Values{"username": {"owner"}, "password": {old}, "confirm": {old}}
	call("/", nil, nil, origin, 303, "/register")
	page := call("/register", nil, nil, origin, 200, "").Body.String()
	if strings.Contains(page, "minlength") || strings.Contains(page, `name="bootstrap"`) {
		t.Fatal("registration form still restricts signup")
	}

	signup.Set("password", "")
	call("/register", signup, nil, origin, 400, "")
	signup.Set("password", old)
	signup.Set("confirm", "different")
	call("/register", signup, nil, origin, 400, "")
	signup.Set("confirm", old)
	call("/register", signup, nil, "http://other.example", 403, "")
	first := call("/register", signup, nil, origin, 303, "/").Result().Cookies()[0]
	if !first.HttpOnly || first.SameSite != http.SameSiteStrictMode {
		t.Fatal("invalid session attributes")
	}
	call("/register", signup, nil, origin, 303, "/login")
	call("/", nil, first, origin, 204, "")
	login := url.Values{"username": {"owner"}, "password": {old}}
	second := call("/login", login, nil, origin, 303, "/").Result().Cookies()[0]
	change := url.Values{"current": {"bad"}, "password": {updated}, "confirm": {updated}}
	call("/password", change, nil, origin, 303, "/login")
	call("/password", change, first, origin, 401, "")
	change.Set("current", old)
	call("/password", change, first, origin, 303, "/login?changed=1")
	for _, c := range []*http.Cookie{first, second} {
		call("/", nil, c, origin, 303, "/login")
	}
	call("/login", login, nil, origin, 401, "")
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credential file permissions", err)
	}
	data, err := os.ReadFile(file)
	if err != nil || strings.Contains(string(data), updated) {
		t.Fatal("credential storage", err)
	}
	h, err = authenticatedAdmin(file, next)
	if err != nil {
		t.Fatal(err)
	}
	call("/register", signup, nil, origin, 303, "/login")
	login.Set("password", updated)
	current := call("/login", login, nil, origin, 303, "/").Result().Cookies()[0]
	call("/", nil, current, origin, 204, "")
	call("/logout", url.Values{}, current, origin, 303, "/login")
	call("/", nil, current, origin, 303, "/login")
}

func TestAdminTCPLogin(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) { testAdminTCPLogin(t, scheme) })
	}
}
func testAdminTCPLogin(t *testing.T, scheme string) {
	g := newGateway()
	g.file = filepath.Join(t.TempDir(), "keys.json")
	h, err := authenticatedAdmin(filepath.Join(filepath.Dir(g.file), "admin-account.json"), http.HandlerFunc(g.admin))
	if err != nil {
		t.Fatal(err)
	}
	public := scheme + "://192.0.2.1:18181"
	call := func(origin, path string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
		method := "GET"
		if form != nil {
			method = "POST"
		}
		r := httptest.NewRequest(method, origin+path, strings.NewReader(form.Encode()))
		var local net.Addr = &net.UnixAddr{Name: "admin.sock", Net: "unix"}
		if origin == public {
			local = &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 18181}
		}
		r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, local))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	account := url.Values{"username": {"owner"}, "password": {"test"}, "confirm": {"test"}}
	if w := call(public, "/", nil, nil); w.Code != 303 || w.Header().Get("Location") != "/register" {
		t.Fatal("first visit", w.Code)
	}
	if w := call(public, "/register", nil, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "注册管理员") {
		t.Fatal("registration form", w.Code)
	}
	if w := call(public, "/register", account, nil); w.Code != 303 || w.Header().Get("Location") != "/" {
		t.Fatal("public registration", w.Code)
	}
	for _, origin := range []string{public, "http://localhost"} {
		if w := call(origin, "/register", account, nil); w.Code != 303 || w.Header().Get("Location") != "/login" {
			t.Fatal("registration after setup", origin, w.Code)
		}
	}
	if w := call(public, "/", nil, nil); w.Header().Get("Location") != "/login" {
		t.Fatal("unauthenticated public access", w.Code)
	}
	w := call(public, "/login", account, nil)
	if w.Code != 303 {
		t.Fatal("login", w.Code)
	}
	cookie := w.Result().Cookies()[0]
	wantName := "key_manager_http_session"
	if scheme == "https" {
		wantName = "key_manager_session"
	}
	if cookie.Name != wantName {
		t.Fatalf("%s session cookie name = %q, want %q", scheme, cookie.Name, wantName)
	}
	if cookie.Secure != (scheme == "https") || !cookie.HttpOnly {
		t.Fatal("public session cookie")
	}
	if w = call(public, "/", nil, cookie); w.Code != 200 || !strings.Contains(w.Body.String(), "API Key 管理") {
		t.Fatal("admin page", w.Code)
	}
}
