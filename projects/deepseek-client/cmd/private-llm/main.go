// private-llm exposes a bounded, SSH-only chat API backed by local Ollama.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

const model = "smollm2:135m"
const maxBody = 32768
const maxText = 4096
const maxContext = 512
const maxOutput = 128

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type request struct {
	Model               string    `json:"model"`
	Messages            []message `json:"messages"`
	Stream              bool      `json:"stream"`
	MaxTokens           int       `json:"max_tokens"`
	MaxCompletionTokens int       `json:"max_completion_tokens"`
	Temperature         *float64  `json:"temperature,omitempty"`
	Options             struct {
		Context int `json:"num_ctx"`
		Predict int `json:"num_predict"`
	} `json:"options"`
}
type gateway struct {
	upstream string
	client   *http.Client
	mu       sync.Mutex
	hits     []time.Time
	slots    chan struct{}
	worker   chan struct{}
}

func newGateway(upstream string) *gateway {
	return &gateway{upstream: upstream, client: &http.Client{Timeout: 150 * time.Second}, slots: make(chan struct{}, 3), worker: make(chan struct{}, 1)}
}
func (g *gateway) admit(now time.Time) (bool, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for len(g.hits) > 0 && !g.hits[0].After(now.Add(-time.Minute)) {
		g.hits = g.hits[1:]
	}
	if len(g.hits) >= 5 {
		return false, int(time.Until(g.hits[0].Add(time.Minute)).Seconds()) + 1
	}
	g.hits = append(g.hits, now)
	return true, 0
}
func send(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
func fail(w http.ResponseWriter, status int, text string) {
	send(w, status, map[string]any{"error": map[string]string{"message": text, "type": "private_llm_error"}})
}
func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		switch r.URL.Path {
		case "/health":
			send(w, 200, map[string]any{"status": "ok", "model": model, "max_context": maxContext, "max_output": maxOutput, "max_input_characters": maxText, "requests_per_minute": 5, "parallel_generations": 1, "queue_capacity": 2, "streaming": false})
			return
		case "/v1/models":
			send(w, 200, map[string]any{"object": "list", "data": []any{map[string]any{"id": model, "object": "model", "owned_by": "local"}}})
			return
		}
	}
	if r.Method != "POST" || (r.URL.Path != "/api/chat" && r.URL.Path != "/v1/chat/completions") {
		fail(w, 404, "Endpoint not available")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		fail(w, 413, "Request body too large or unreadable")
		return
	}
	var p request
	if err = json.Unmarshal(data, &p); err != nil {
		fail(w, 400, "Invalid JSON or field type")
		return
	}
	if p.Model != "" && p.Model != model {
		fail(w, 400, "Only "+model+" is allowed")
		return
	}
	if p.Stream {
		fail(w, 400, "Streaming is not supported; use stream=false")
		return
	}
	if p.Options.Context < 0 || p.Options.Context > maxContext {
		fail(w, 400, "Maximum context is 512 tokens")
		return
	}
	if p.Options.Predict < 0 || p.Options.Predict > maxOutput || p.MaxTokens < 0 || p.MaxTokens > maxOutput || p.MaxCompletionTokens < 0 || p.MaxCompletionTokens > maxOutput {
		fail(w, 400, "Maximum output is 128 tokens")
		return
	}
	if p.Temperature != nil && (*p.Temperature < 0 || *p.Temperature > 2) {
		fail(w, 400, "Temperature must be between 0 and 2")
		return
	}
	if len(p.Messages) == 0 || len(p.Messages) > 32 {
		fail(w, 400, "Expected 1 to 32 messages")
		return
	}
	count := 0
	for _, m := range p.Messages {
		if m.Role != "user" && m.Role != "assistant" && m.Role != "system" {
			fail(w, 400, "Only text chat roles are supported")
			return
		}
		count += len([]rune(m.Content))
	}
	if count > maxText {
		fail(w, 413, "Maximum total message length is 4096 characters")
		return
	}
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	default:
		fail(w, 503, "Queue is full; retry later")
		return
	}
	if ok, retry := g.admit(time.Now()); !ok {
		w.Header().Set("Retry-After", fmt.Sprint(retry))
		fail(w, 429, "Rate limit: 5 requests per minute")
		return
	}
	select {
	case g.worker <- struct{}{}:
		defer func() { <-g.worker }()
	case <-r.Context().Done():
		return
	}
	output := maxOutput
	for _, n := range []int{p.MaxTokens, p.MaxCompletionTokens, p.Options.Predict} {
		if n > 0 && n < output {
			output = n
		}
	}
	options := map[string]any{"num_ctx": maxContext, "num_predict": output}
	if p.Temperature != nil {
		options["temperature"] = *p.Temperature
	}
	// Rebuild the payload: arbitrary client options cannot reach the backend.
	payload := map[string]any{"model": model, "messages": p.Messages, "stream": false, "options": options, "keep_alive": "5m"}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(r.Context(), "POST", g.upstream+"/api/chat", bytes.NewReader(body))
	if err != nil {
		fail(w, 502, "Backend configuration error")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		fail(w, 502, "Local model unavailable or timed out")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fail(w, 502, "Local model returned an error")
		return
	}
	var answer struct {
		Message message `json:"message"`
		Prompt  int     `json:"prompt_eval_count"`
		Output  int     `json:"eval_count"`
		Reason  string  `json:"done_reason"`
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil || json.Unmarshal(raw, &answer) != nil {
		fail(w, 502, "Invalid backend response")
		return
	}
	if r.URL.Path == "/api/chat" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
		return
	}
	finish := "stop"
	if answer.Reason == "length" {
		finish = "length"
	}
	send(w, 200, map[string]any{"id": fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()), "object": "chat.completion", "created": time.Now().Unix(), "model": model, "choices": []any{map[string]any{"index": 0, "message": answer.Message, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": answer.Prompt, "completion_tokens": answer.Output, "total_tokens": answer.Prompt + answer.Output}})
}
func main() {
	upstream := os.Getenv("OLLAMA_UPSTREAM")
	if upstream == "" {
		upstream = "http://127.0.0.1:11434"
	}
	server := &http.Server{Addr: "127.0.0.1:11436", Handler: newGateway(upstream), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 180 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	log.Print("Private LLM API on 127.0.0.1:11436; access via SSH tunnel")
	log.Fatal(server.ListenAndServe())
}
