package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClientParametersWithLegacyKey(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tokenize" {
			var fields rawObject
			if err := json.NewDecoder(r.Body).Decode(&fields); err != nil || fields["stream"] != nil || fields["stream_options"] != nil || fields["tools"] == nil {
				t.Error("invalid tokenizer fields", err)
			}
			w.Write([]byte(`{"count":6}`))
			return
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		for _, name := range []string{"temperature", "top_p", "tools", "tool_choice", "response_format", "user", "thinking", "chat_template_kwargs", "min_tokens", "stop_token_ids"} {
			if body[name] == nil {
				t.Errorf("lost %s", name)
			}
		}
		if string(body["max_completion_tokens"]) != "10000" || body["max_tokens"] != nil {
			t.Error("client output length changed")
		}
		var options map[string]bool
		json.Unmarshal(body["stream_options"], &options)
		if !options["include_usage"] || !options["continuous_usage_stats"] {
			t.Error("invalid stream options")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"usage\":{\"total_tokens\":22}}\n\ndata: [DONE]\n\n"))
	}))
	defer up.Close()
	g := newGateway()
	g.file = filepath.Join(t.TempDir(), "keys")
	g.upstream = up.URL
	if err := g.update(func(m map[string]*keyRecord) error {
		m[hashKey("sk-fixture")] = &keyRecord{Limit: 20000, Expires: time.Now().Add(time.Hour), RPM: 10, Concurrent: 1, LegacyOutput: 2048}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"glm","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"disabled"},"chat_template_kwargs":{"enable_thinking":false},"min_tokens":1,"stop_token_ids":[],"temperature":0.6,"top_p":0.9,"max_completion_tokens":10000,"tools":[],"tool_choice":"none","response_format":{"type":"text"},"user":"sample","stream":true,"stream_options":{"include_usage":false,"continuous_usage_stats":true}}`))
	r.Header.Set("Authorization", "Bearer sk-fixture")
	w := httptest.NewRecorder()
	g.public(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := g.withRecords(false, func(m map[string]*keyRecord) error {
		if m[hashKey("sk-fixture")].Used != 22 {
			t.Error("usage mismatch")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestChatValidation(t *testing.T) {
	for _, tc := range []struct {
		body, param string
		code        int
	}{
		{`null`, "body", 400},
		{`{"stream_options":4,"stream":true}`, "stream_options", 400},
		{`{"model":"glm","messages":[{}],"n":0}`, "n", 400},
		{`{"model":"glm","messages":[{}],"max_completion_tokens":-1}`, "max_completion_tokens", 400},
		{`{"model":"glm","messages":[{}],"bootstrap_host":1}`, "bootstrap_host", 400},
		{`{}{}`, "body", 400}, {strings.Repeat(" ", (1<<20)+1), "body", 413},
	} {
		w := httptest.NewRecorder()
		_, ok := readChat(w, httptest.NewRequest("POST", "/", strings.NewReader(tc.body)), 1<<20)
		var payload struct{ Error struct{ Param string } }
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil || ok || w.Code != tc.code || payload.Error.Param != tc.param {
			t.Errorf("expected %s/%d, got %d %s", tc.param, tc.code, w.Code, w.Body.String())
		}
	}
}

func TestModelDefaultOutputAndQuota(t *testing.T) {
	const secret = "sk-fixture"
	for _, tc := range []struct {
		name, params, wantMax, wantCompletion string
		balance, hold                         int64
	}{
		{"omitted, default model", `"n":2`, "", "", 50000, 16381},
		{"omitted, named model", `"model":"second"`, "", "", 50000, 16000},
		{"omitted, low quota", `"model":"second","n":2`, "8", "", 20, 19},
		{"explicit client limit", `"max_tokens":4096`, "4096", "", 20000, 4099},
		{"upstream alias precedence", `"max_tokens":16,"max_completion_tokens":4096`, "16", "4096", 20000, 4099},
		{"zero output", `"max_tokens":0`, "0", "", 3, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGateway()
			g.file = filepath.Join(t.TempDir(), "keys.json")
			hash := hashKey(secret)
			if err := g.update(func(m map[string]*keyRecord) error {
				m[hash] = &keyRecord{LegacyOutput: 2048, Limit: tc.balance, RPM: 10, Concurrent: 2, Expires: time.Now().Add(time.Hour)}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/models":
					w.Write([]byte(`{"data":[{"id":"first","max_model_len":8192},{"id":"second","max_model_len":16000}]}`))
				case "/v1/tokenize":
					w.Write([]byte(`{"count":3,"max_model_len":1048576}`))
				case "/v1/chat/completions":
					var fields rawObject
					if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
						t.Error(err)
					}
					if string(fields["max_tokens"]) != tc.wantMax || string(fields["max_completion_tokens"]) != tc.wantCompletion {
						t.Errorf("forwarded limits %s, %s", fields["max_tokens"], fields["max_completion_tokens"])
					}
					g.mu.Lock()
					if held := g.windows[hash].held; held != tc.hold {
						t.Errorf("reserved %d, want %d", held, tc.hold)
					}
					g.mu.Unlock()
					w.Write([]byte(`{"usage":{"total_tokens":3}}`))
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			}))
			defer up.Close()
			g.upstream = up.URL
			r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"hi"}],`+tc.params+"}"))
			r.Header.Set("Authorization", "Bearer "+secret)
			w := httptest.NewRecorder()
			g.public(w, r)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
