package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestExternalTrialKeysAndPublicAddress(t *testing.T) {
	dir := t.TempDir()
	db, err := openDB(filepath.Join(dir, "console.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer upstream.Close()
	dest, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	app := newConsole(db, dest)
	app.externalOnly = true
	app.publicFile = filepath.Join(dir, "origin")
	for _, test := range []struct{ value, want string }{
		{"http://127.0.0.1:18080", ""},
		{"https://localhost", ""},
		{"https://preview.example/", "https://preview.example"},
		{"https://rotated.example", "https://rotated.example"},
	} {
		if err := os.WriteFile(app.publicFile, []byte(test.value), 0600); err != nil {
			t.Fatal(err)
		}
		if got, _ := app.publicOrigin(); got != test.want {
			t.Fatalf("origin %q: got %q", test.value, got)
		}
	}
	if err := os.WriteFile(app.publicFile, []byte("https://192.0.2.1\npending\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if address, note := app.publicOrigin(); address != "https://192.0.2.1" || note != "pending" {
		t.Fatalf("IP endpoint: got %q, %q", address, note)
	}
	for i, name := range []string{"local-user", "invited-user"} {
		id := i + 1
		if _, err := db.Exec("INSERT INTO users(id,name) VALUES(?,?)", id, name); err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			if _, err := db.Exec("INSERT INTO external_users(user_id) VALUES(?)", id); err != nil {
				t.Fatal(err)
			}
		}
		key := "sk-glm-" + newToken()
		if _, err := db.Exec("INSERT INTO api_keys(owner,label,digest,prefix,created) VALUES(?,'test',?,'prefix',0)", id, digest(key)); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodGet, "http://trial.example/v1/models", nil)
		r.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		app.routes().ServeHTTP(w, r)
		want := http.StatusUnauthorized
		if i == 1 {
			want = http.StatusOK
		}
		if w.Code != want {
			t.Errorf("%s: got %d, want %d", name, w.Code, want)
		}
	}
}

func TestBrowserFormOrigin(t *testing.T) {
	handler := (&console{}).routes()
	for _, tc := range []struct {
		name, origin, site string
		want               int
	}{
		{"browser with no-referrer", "null", "same-origin", http.StatusSeeOther},
		{"browser with origin", "http://127.0.0.1:18080", "same-origin", http.StatusSeeOther},
		{"client without fetch metadata", "http://127.0.0.1:18080", "", http.StatusSeeOther},
		{"cross-site form", "https://other.example", "cross-site", http.StatusForbidden},
		{"opaque cross-site form", "null", "cross-site", http.StatusForbidden},
		{"foreign origin", "https://other.example", "", http.StatusForbidden},
		{"missing origin and fetch metadata", "", "", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18080/ui/logout", nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("got %d, want %d", w.Code, tc.want)
			}
		})
	}
}
