package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGuardsAndCanonicalPayload(t *testing.T) {
	var calls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		o := p["options"].(map[string]any)
		if o["num_ctx"] != float64(512) || o["num_predict"] != float64(128) || o["num_gpu"] != nil {
			t.Error("client bypassed server options", o)
		}
		_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"hello"},"prompt_eval_count":7,"eval_count":1,"done_reason":"stop"}`)
	}))
	defer backend.Close()
	g := newGateway(backend.URL)
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"messages":[{"role":"user","content":"hi"}],"options":{"num_ctx":8192}}`, 400},
		{`{"messages":[{"role":"user","content":"hi"}],"max_tokens":129}`, 400},
		{`{"messages":[{"role":"user","content":"hi"}],"options":{"num_predict":-1}}`, 400},
		{`{"messages":[{"role":"user","content":"hi"}],"stream":true}`, 400},
		{`{"messages":[{"role":"user","content":"` + strings.Repeat("a", 4097) + `"}]}`, 413},
		{strings.Repeat("x", maxBody+1), 413},
		{`{"model":"other","messages":[]}`, 400},
	} {
		w := httptest.NewRecorder()
		g.ServeHTTP(w, httptest.NewRequest("POST", "/api/chat", strings.NewReader(tc.body)))
		if w.Code != tc.status {
			t.Fatalf("got %d want %d", w.Code, tc.status)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("rejected request reached model")
	}
	body := `{"messages":[{"role":"user","content":"hi"}],"options":{"num_gpu":99}}`
	for i := 0; i < 6; i++ {
		w := httptest.NewRecorder()
		g.ServeHTTP(w, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
		want := 200
		if i == 5 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("request %d got %d", i, w.Code)
		}
		if i == 0 && !strings.Contains(w.Body.String(), `"choices"`) {
			t.Fatal("OpenAI response missing choices")
		}
		if i == 5 && w.Header().Get("Retry-After") == "" {
			t.Fatal("missing retry header")
		}
	}
	if calls.Load() != 5 {
		t.Fatal("rate limit did not protect backend")
	}
	g.mu.Lock()
	g.hits = []time.Time{time.Now().Add(-61 * time.Second)}
	g.mu.Unlock()
	if ok, _ := g.admit(time.Now()); !ok {
		t.Fatal("rate window did not expire")
	}
}
func TestConcurrentRequestsSerialized(t *testing.T) {
	var active atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if active.Add(1) != 1 {
			t.Error("concurrent backend generations")
		}
		defer active.Add(-1)
		time.Sleep(25 * time.Millisecond)
		_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"hello"}}`)
	}))
	defer backend.Close()
	g := newGateway(backend.URL)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			g.ServeHTTP(w, httptest.NewRequest("POST", "/api/chat", strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`)))
			if w.Code != 200 {
				t.Error(w.Code)
			}
		}()
	}
	wg.Wait()
}
