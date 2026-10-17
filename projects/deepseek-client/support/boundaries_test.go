package support

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

func TestProviderFailureKeepsBudget(t *testing.T) {
	s := testService(t, "U-100", agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
		return agent.Completion{}, errors.New("private upstream credential must not escape")
	}))
	_, e := s.Ask(context.Background(), AskRequest{"Почему не работает авторизация?", "T-100"})
	if e == nil || strings.Contains(e.Error(), "private upstream") {
		t.Fatal("upstream failure leaked")
	}
	usage := s.Budget.Snapshot()
	if usage.Calls != 1 || usage.Reserved <= 0 {
		t.Fatal("failed request reservation lost")
	}
}
func TestParallelHTTPDoesNotSendSecondModelRequest(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	client := agent.ClientFunc(func(context.Context, agent.Target, string, agent.Settings) (agent.Completion, error) {
		close(entered)
		<-release
		return agent.Completion{Content: "{}", FinishReason: "stop"}, nil
	})
	s := testService(t, "U-100", client)
	token := strings.Repeat("a", 64)
	handler := Handler(s, token, "127.0.0.1:8766", nil)
	call := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8766/api/ask", strings.NewReader("{\"question\":\"Почему не работает авторизация?\",\"ticket_id\":\"T-100\"}"))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	done := make(chan struct{})
	go func() { call(); close(done) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not reach provider")
	}
	second := call()
	close(release)
	<-done
	if second.Code != 429 || s.Budget.Snapshot().Calls != 1 {
		t.Fatal("parallel request reached provider")
	}
}
