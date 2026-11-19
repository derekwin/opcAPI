package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuotaAndUpstreamFailure(t *testing.T) {
	const secret = "sk-test"
	const prompt = 15
	var genCalls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tokenize" {
			fmt.Fprint(w, `{"count":15}`)
			return
		}
		if r.URL.Path == "/v1/models/glm" {
			fmt.Fprint(w, `{"id":"glm"}`)
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Error("unexpected route", r.URL.Path)
		}
		var payload rawObject
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if string(payload["max_tokens"]) != "2" || string(payload["n"]) != "2" || payload["model"] != nil {
			t.Error("wrong forwarding", payload)
		}
		genCalls.Add(1)
		var mode string
		if err := json.Unmarshal(payload["user"], &mode); err != nil {
			t.Error(err)
		}
		switch mode {
		case "disconnect":
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			conn.Close()
		case "busy":
			http.Error(w, "busy", 503)
		case "invalid":
			http.Error(w, "invalid", 400)
		case "missing usage":
			fmt.Fprint(w, `{"choices":[]}`)
		default:
			fmt.Fprint(w, `{"usage":{"prompt_tokens":15,"total_tokens":19}}`)
		}
	}))
	defer up.Close()
	g := newGateway()
	g.file = filepath.Join(t.TempDir(), "keys.json")
	g.upstream = up.URL
	h := hashKey(secret)
	if err := g.update(func(m map[string]*keyRecord) error {
		m[h] = &keyRecord{Limit: prompt + 5, Expires: time.Now().Add(time.Hour), RPM: 30, Concurrent: 1}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	call := func(path, body string) *httptest.ResponseRecorder {
		method := http.MethodPost
		if body == "" {
			method = http.MethodGet
		}
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		g.public(w, req)
		return w
	}
	body := `{"messages":[{"role":"user","content":"hi"}],"n":2,"max_completion_tokens":20,"min_tokens":1,"user":%q}`
	chat := "/v1/chat/completions"
	for _, tc := range []struct {
		mode   string
		status int
		used   int64
	}{
		{"disconnect", 502, 0}, {"busy", 503, 0}, {"invalid", 400, 0}, {"missing usage", 502, 0}, {"success", 200, 19}, {"insufficient", 429, 19},
	} {
		w := call(chat, fmt.Sprintf(body, tc.mode)+strings.Repeat(" ", (1<<20)+1))
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.mode, w.Code, w.Body.String())
		}
		if err := g.withRecords(false, func(m map[string]*keyRecord) error {
			if k := m[h]; k.Used != tc.used || g.windows[h].held != 0 {
				t.Fatalf("%s: used %d held %d", tc.mode, k.Used, g.windows[h].held)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if genCalls.Load() != 5 {
		t.Fatal("unexpected generation count", genCalls.Load())
	}
	if w := call("/v1/models/glm", ""); w.Code != 200 || w.Body.String() != `{"id":"glm"}` {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestStreamOptionsAndInterruptedBilling(t *testing.T) {
	const deltas = "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{}]}}],\"usage\":{\"prompt_tokens\":15,\"completion_tokens\":2,\"total_tokens\":17}}\n\n" +
		"data: {\"choices\":[{\"index\":1}],\"usage\":{\"prompt_tokens\":15,\"completion_tokens\":3,\"total_tokens\":18}}\n\n"
	const final = "data: {\"choices\":[],\"usage\":{\"total_tokens\":20}}\n\n"
	const done = "data: [DONE]\n\n"
	for _, include := range []bool{false, true} {
		for _, continuous := range []bool{false, true} {
			w := httptest.NewRecorder()
			n := relayStream(w, strings.NewReader(deltas+final+done), &chatRequest{includeUsage: include, continuous: continuous})
			got := w.Body.String()
			if n != 20 || strings.Contains(got, `"choices":[]`) != include || strings.Contains(got, `"prompt_tokens"`) != continuous || !strings.Contains(got, "tool_calls") || !strings.HasSuffix(got, done) {
				t.Fatalf("include=%v continuous=%v, charged=%d, stream=%s", include, continuous, n, got)
			}
		}
	}
	if total := relayStream(httptest.NewRecorder(), strings.NewReader(deltas), &chatRequest{}); total != 20 {
		t.Fatalf("interrupted stream charged %d", total)
	}
}
