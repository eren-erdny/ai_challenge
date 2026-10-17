package support

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/mcptools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testService(t *testing.T, user string, client agent.Client) *Service {
	t.Helper()
	db, e := LoadDatabase("../../../examples/support-assistant/crm.json")
	if e != nil {
		t.Fatal(e)
	}
	server, e := NewCRM(db, user)
	if e != nil {
		t.Fatal(e)
	}
	a, b := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	session, e := server.Connect(ctx, a, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { session.Close() })
	crm, e := mcptools.Connect(ctx, b, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { crm.Close() })
	corpus, e := LoadCorpus("../../../examples/support-assistant/docs")
	if e != nil {
		t.Fatal(e)
	}
	budget, e := OpenBudget(filepath.Join(t.TempDir(), "budget.json"), .20)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { budget.Close() })
	return &Service{CRM: crm, Corpus: corpus, Client: client, Budget: budget, BoundUser: user}
}
func TestMCPIsolationAndArguments(t *testing.T) {
	s := testService(t, "U-100", nil)
	list, e := s.Tickets(context.Background())
	if e != nil || len(list.Tickets) != 2 {
		t.Fatalf("own tickets: %v %+v", e, list)
	}
	for _, raw := range []string{
		"{\"ticket_id\":\"T-200\"}",
		"{\"ticket_id\":\"../../secret\"}",
		"{\"ticket_id\":\"T-100\",\"user_id\":\"U-200\"}",
		"{\"ticket_id\":\"T-100\",\"ticket_id\":\"T-200\"}",
	} {
		if _, e = s.CRM.Execute(context.Background(), "support_get_ticket", raw); e == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if _, e = s.CRM.Execute(context.Background(), "support_get_user", "{\"user_id\":\"U-200\"}"); e == nil {
		t.Fatal("user override accepted")
	}
	if _, e = s.CRM.Execute(context.Background(), "support_delete_ticket", "{}"); e == nil {
		t.Fatal("write tool accepted")
	}
	reply, e := s.Ask(context.Background(), AskRequest{"Почему не работает авторизация?", "T-200"})
	if e == nil || reply.Usage.Calls != 0 || s.Budget.Snapshot().Calls != 0 {
		t.Fatal("foreign ticket reached provider")
	}
}
func TestRetrievalAndUnknown(t *testing.T) {
	s := testService(t, "U-100", nil)
	docs := s.Corpus.Search("авторизация AUTH_EMAIL_UNVERIFIED")
	if len(docs) == 0 || !strings.Contains(docs[0].Text, "email_verified=false") {
		t.Fatal("wrong auth retrieval")
	}
	locked := s.Corpus.Search("авторизация ACCOUNT_LOCKED")
	if len(locked) == 0 || !strings.Contains(locked[0].Text, "15 минут") {
		t.Fatal("wrong lock retrieval")
	}
	result, e := s.Ask(context.Background(), AskRequest{Question: "Погода на Юпитере завтра"})
	if e != nil || result.Answer.Status != "unknown" || s.Budget.Snapshot().Calls != 0 {
		t.Fatal("unsupported question called provider")
	}
}
func TestGroundedContextAndBudget(t *testing.T) {
	var service *Service
	calls := 0
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, settings agent.Settings) (agent.Completion, error) {
		calls++
		if !settings.JSONOutput || len(settings.Tools) != 0 || settings.MaxOutputTokens != 1200 || !settings.SendThinkingDisabled {
			t.Fatal("unsafe generation settings")
		}
		all := ""
		for _, m := range settings.Messages {
			all += m.Content
		}
		if !strings.Contains(all, "AUTH_EMAIL_UNVERIFIED") || !strings.Contains(all, "crm-ticket") {
			t.Fatal("missing current MCP context")
		}
		if service.Budget.usage.Reserved <= 0 {
			t.Fatal("unreserved provider request")
		}
		var saved Usage
		data, e := os.ReadFile(service.Budget.path)
		if e != nil || json.Unmarshal(data, &saved) != nil || saved.Calls != 1 {
			t.Fatal("reservation not durable")
		}
		var doc Chunk
		for _, c := range service.Corpus.Chunks {
			if strings.Contains(c.Text, "Если email_verified=false") {
				doc = c
			}
		}
		raw, _ := json.Marshal(map[string]any{"status": "answered", "claims": []any{map[string]any{"text": "В тикете AUTH_EMAIL_UNVERIFIED; подтвердите почту.", "quote_ids": []string{"q1", "q2"}}}, "quotes": []any{
			map[string]any{"id": "q1", "chunk_id": "crm-ticket", "text": "AUTH_EMAIL_UNVERIFIED"},
			map[string]any{"id": "q2", "chunk_id": doc.ID, "text": "Если email_verified=false, откройте письмо TaskDesk и перейдите по ссылке подтверждения."}}, "clarification": ""})
		return agent.Completion{Content: string(raw), FinishReason: "stop", UsageKnown: true, PromptTokens: 100, CompletionTokens: 100}, nil
	})
	service = testService(t, "U-100", client)
	reply, e := service.Ask(context.Background(), AskRequest{"Почему не работает авторизация?", "T-100"})
	if e != nil {
		t.Fatal(e)
	}
	if reply.Answer.Status != "answered" || len(reply.Answer.Sources) != 2 || calls != 1 || reply.Usage.EstimatedPeak <= 0 {
		t.Fatalf("missing grounded answer/usage: status=%s sources=%d calls=%d usage=%+v", reply.Answer.Status, len(reply.Answer.Sources), calls, reply.Usage)
	}
	service.Budget.mu.Lock()
	service.Budget.usage.Reserved = .19999
	service.Budget.mu.Unlock()
	if _, e = service.Ask(context.Background(), AskRequest{"Почему не работает авторизация?", "T-100"}); e == nil || calls != 1 {
		t.Fatal("overspent budget")
	}
}
func TestHTTPBoundary(t *testing.T) {
	s := testService(t, "U-100", nil)
	token := strings.Repeat("a", 64)
	handler := Handler(s, token, "127.0.0.1:8766", nil)
	request := func(method, path, body, auth, origin, host string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:8766"+path, strings.NewReader(body))
		r.Host = host
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", auth)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	valid := "Bearer " + token
	for _, test := range []struct {
		auth, origin, host string
		code               int
	}{
		{"", "", "127.0.0.1:8766", 401},
		{valid, "http://evil.invalid", "127.0.0.1:8766", 403},
		{valid, "", "evil.invalid:8766", 403},
	} {
		if w := request("GET", "/api/tickets", "", test.auth, test.origin, test.host); w.Code != test.code {
			t.Errorf("boundary: %d", w.Code)
		}
	}
	if w := request("GET", "/api/tickets", "", valid, "", "127.0.0.1:8766"); w.Code != 200 || strings.Contains(w.Body.String(), "T-200") {
		t.Fatal("ticket disclosure")
	}
	if w := request("POST", "/api/ask", "{\"question\":\"x\",\"user_id\":\"U-200\"}", valid, "", "127.0.0.1:8766"); w.Code != 400 {
		t.Fatal("unknown request fields accepted")
	}
	w := request("GET", "/", "", "", "", "127.0.0.1:8766")
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "nonce-") || strings.Contains(w.Body.String(), "{{NONCE}}") {
		t.Fatal("UI CSP")
	}
}
func TestStrictJSONAndLedger(t *testing.T) {
	for _, raw := range []string{"null", "{} {}", "{\"x\":1,\"x\":2}"} {
		var value map[string]any
		if strict([]byte(raw), &value) == nil {
			t.Fatal("invalid JSON accepted")
		}
	}
	p := filepath.Join(t.TempDir(), "budget.json")
	budget, e := OpenBudget(p, .20)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = OpenBudget(p, .20); e == nil {
		t.Fatal("parallel ledger opened")
	}
	budget.Close()
	if _, e = OpenBudget(p, 1); e == nil {
		t.Fatal("excessive budget accepted")
	}
}
