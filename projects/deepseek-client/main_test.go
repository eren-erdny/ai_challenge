package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

func TestAgentCallsHTTPClientWithPreparedMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("incorrect HTTP request or authentication")
		}
		var payload chatRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.Model != "test-model" || len(payload.Messages) != 2 || payload.Messages[1].Content != "question" || payload.MaxTokens != 200 || len(payload.Stop) != 1 {
			t.Errorf("incorrect payload: %+v", payload)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"summary\":\"ok\",\"points\":[\"one\"]}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":4}}}`)
	}))
	defer server.Close()
	runner := agent.New(agent.ClientFunc(func(ctx context.Context, _ agent.Target, prompt string, settings agent.Settings) (agent.Completion, error) {
		return askDeepSeek(ctx, "test-secret", prompt, settings)
	}))
	result, err := runner.Run(context.Background(), agent.Request{Prompt: "question", Mode: agent.Controlled,
		Target:  agent.Target{Model: "test-model", BaseURL: server.URL + "/v1"},
		Control: agent.ControlConfig{Enabled: true, Format: "json", MaxWords: 80, MaxTokens: 200, StopSequences: []string{"<END>"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	answer := result.Responses[0]
	if answer.Validation == nil || !answer.Validation.Passed || answer.Answer.TotalTokens != 15 || answer.Answer.CachedInputTokens != 4 {
		t.Fatalf("unexpected result: %+v", answer)
	}
}

func TestAgentCancellationReachesHTTP(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := agent.New(agent.ClientFunc(func(ctx context.Context, _ agent.Target, prompt string, settings agent.Settings) (agent.Completion, error) {
		return askDeepSeek(ctx, "", prompt, settings)
	}))
	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx, agent.Request{Prompt: "question", Target: agent.Target{Model: "test-model", BaseURL: server.URL + "/v1"}})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request never reached server")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP request did not stop after cancellation")
	}
}

func TestBuildChatRequestsUseSameUserPrompt(t *testing.T) {
	const prompt = "Объясни преимущества языка Go"
	control, err := buildResponseControl(defaultAppConfig().ResponseControl)
	if err != nil {
		t.Fatalf("buildResponseControl() returned error: %v", err)
	}

	uncontrolled := buildChatRequest(prompt, requestSettings{Model: defaultModelName, Temperature: 0.7, Strategy: strategyStandard})
	controlled := buildChatRequest(prompt, requestSettings{Model: defaultModelName, Temperature: 0.7, Strategy: strategyStandard, Control: control})

	if got := uncontrolled.Messages[len(uncontrolled.Messages)-1].Content; got != prompt {
		t.Fatalf("uncontrolled prompt = %q, want %q", got, prompt)
	}
	if got := controlled.Messages[len(controlled.Messages)-1].Content; got != prompt {
		t.Fatalf("controlled prompt = %q, want %q", got, prompt)
	}
}

func TestBuildChatRequestAppliesControlsOnlyToSecondRequest(t *testing.T) {
	control, err := buildResponseControl(defaultAppConfig().ResponseControl)
	if err != nil {
		t.Fatalf("buildResponseControl() returned error: %v", err)
	}

	uncontrolled := buildChatRequest("test", requestSettings{Model: defaultModelName, Temperature: 0.7, Strategy: strategyStandard})
	controlled := buildChatRequest("test", requestSettings{Model: defaultModelName, Temperature: 0.7, Strategy: strategyStandard, Control: control})

	uncontrolledJSON, err := json.Marshal(uncontrolled)
	if err != nil {
		t.Fatalf("marshal uncontrolled request: %v", err)
	}
	if strings.Contains(string(uncontrolledJSON), "max_tokens") || strings.Contains(string(uncontrolledJSON), `"stop"`) {
		t.Fatalf("uncontrolled request contains output controls: %s", uncontrolledJSON)
	}

	if controlled.MaxTokens != 200 {
		t.Fatalf("controlled max_tokens = %d, want 200", controlled.MaxTokens)
	}
	if len(controlled.Stop) != 1 || controlled.Stop[0] != "<END>" {
		t.Fatalf("controlled stop = %v, want [<END>]", controlled.Stop)
	}
	if len(controlled.Messages) != 2 || controlled.Messages[0].Role != "system" {
		t.Fatalf("controlled messages = %#v, want system and user messages", controlled.Messages)
	}
}

func TestBuildChatRequestAppliesTemperatureAndStrategy(t *testing.T) {
	request := buildChatRequest("test", requestSettings{
		Model:                "deepseek-v4-pro",
		SendThinkingDisabled: true,
		Temperature:          1.2,
		Strategy:             strategyExperts,
	})
	if request.Temperature != 1.2 {
		t.Fatalf("temperature = %g, want 1.2", request.Temperature)
	}
	if request.Model != "deepseek-v4-pro" {
		t.Fatalf("model = %q, want deepseek-v4-pro", request.Model)
	}
	if request.Thinking == nil || request.Thinking.Type != "disabled" {
		t.Fatalf("thinking = %q, want disabled", request.Thinking.Type)
	}
	if len(request.Messages) != 2 || !strings.Contains(request.Messages[0].Content, "Аналитик") ||
		!strings.Contains(request.Messages[0].Content, "Инженер") || !strings.Contains(request.Messages[0].Content, "Критик") {
		t.Fatalf("expert strategy system message is incomplete: %#v", request.Messages)
	}
}

func TestBuildChatRequestOmitsThinkingForGenericAPI(t *testing.T) {
	request := buildChatRequest("test", requestSettings{Model: "local-model", Temperature: 0.7})
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if strings.Contains(string(encoded), `"thinking"`) {
		t.Fatalf("generic request contains DeepSeek-specific thinking: %s", encoded)
	}
}

func TestChatCompletionsURL(t *testing.T) {
	for input, want := range map[string]string{
		"http://127.0.0.1:1234/v1":             "http://127.0.0.1:1234/v1/chat/completions",
		"https://api.deepseek.com/":            "https://api.deepseek.com/chat/completions",
		"http://localhost/v1/chat/completions": "http://localhost/v1/chat/completions",
	} {
		if got := chatCompletionsURL(input); got != want {
			t.Fatalf("chatCompletionsURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAskDeepSeekSupportsLocalAPIWithoutToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Errorf("request path = %q", request.URL.Path)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "" {
			t.Errorf("unexpected Authorization header: %q", authorization)
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if _, exists := payload["thinking"]; exists {
			t.Errorf("local request contains thinking: %#v", payload)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"model":"local-model","choices":[{"message":{"role":"assistant","content":"Локальный ответ"},"finish_reason":"stop"}],"usage":{"completion_tokens":2}}`))
	}))
	defer server.Close()

	result, err := askDeepSeek(context.Background(), "", "Привет", requestSettings{
		Model:       "local-model",
		BaseURL:     server.URL + "/v1",
		Temperature: 0.7,
	})
	if err != nil {
		t.Fatalf("askDeepSeek returned error: %v", err)
	}
	if result.Content != "Локальный ответ" || result.Model != "local-model" {
		t.Fatalf("result = %#v", result)
	}
}

func TestFetchModelsUsesOpenAICompatibleEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/models" {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "Bearer test-key" {
			t.Errorf("Authorization = %q", authorization)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"data":[{"id":"qwen-local"},{"id":"llama-local"}]}`))
	}))
	defer server.Close()

	models, err := fetchModels(context.Background(), "test-key", apiProfile{BaseURL: server.URL + "/v1", Model: "qwen-local"})
	if err != nil {
		t.Fatalf("fetchModels() returned error: %v", err)
	}
	if got := strings.Join(models, ","); got != "llama-local,qwen-local" {
		t.Fatalf("models = %q", got)
	}
}

func TestTokensPerSecond(t *testing.T) {
	result := completionResult{CompletionTokens: 90, Duration: 3 * time.Second}
	if got := tokensPerSecond(result); got != 30 {
		t.Fatalf("tokensPerSecond() = %g, want 30", got)
	}
}

func TestHandleListModelsCommand(t *testing.T) {
	var output strings.Builder
	var errorOutput strings.Builder

	handled, exitCode := handleCommand([]string{"--list-models"}, &output, &errorOutput)
	if !handled || exitCode != 0 || errorOutput.Len() != 0 {
		t.Fatalf("handleCommand() = (%v, %d), stderr=%q", handled, exitCode, errorOutput.String())
	}
	if !strings.Contains(output.String(), "deepseek-v4-flash") || !strings.Contains(output.String(), "deepseek-v4-pro") {
		t.Fatalf("model catalog is incomplete: %q", output.String())
	}
}

func TestHandleListFormatsCommand(t *testing.T) {
	var output strings.Builder
	var errorOutput strings.Builder

	handled, exitCode := handleCommand([]string{"--list-formats"}, &output, &errorOutput)

	if !handled || exitCode != 0 {
		t.Fatalf("handleCommand() = (%v, %d), want (true, 0)", handled, exitCode)
	}
	if !strings.Contains(output.String(), "structured_markdown") || !strings.Contains(output.String(), "json") {
		t.Fatalf("format catalog is incomplete: %q", output.String())
	}
	if errorOutput.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", errorOutput.String())
	}
}

func TestWordCount(t *testing.T) {
	if got, want := wordCount("Короткий ответ из четырёх слов"), 5; got != want {
		t.Fatalf("wordCount() = %d, want %d", got, want)
	}
}

func TestReadPrompt(t *testing.T) {
	input := bufio.NewReader(strings.NewReader("Расскажи про интерфейсы в Go\n"))
	prompt, err := readPrompt(input)
	if err != nil {
		t.Fatalf("readPrompt() returned error: %v", err)
	}

	if want := "Расскажи про интерфейсы в Go"; prompt != want {
		t.Fatalf("readPrompt() = %q, want %q", prompt, want)
	}
}

func TestReadPromptRejectsEmptyInput(t *testing.T) {
	input := bufio.NewReader(strings.NewReader("   \n"))
	if _, err := readPrompt(input); err == nil {
		t.Fatal("readPrompt() must reject empty input")
	}
}

func TestWaitForEnter(t *testing.T) {
	input := bufio.NewReader(strings.NewReader("\n"))
	var output strings.Builder

	waitForEnter(input, &output)

	if want := "Нажмите Enter, чтобы закрыть программу"; !strings.Contains(output.String(), want) {
		t.Fatalf("waitForEnter() output = %q, must contain %q", output.String(), want)
	}
}

func TestFormatAnswer(t *testing.T) {
	input := "  ## Go  \r\n\r\n\r\nТекст ответа.   \r\n\r\n- пункт\t\r\n  "
	want := "## Go\n\nТекст ответа.\n\n- пункт"

	if got := formatAnswer(input); got != want {
		t.Fatalf("formatAnswer() = %q, want %q", got, want)
	}
}

func TestPrintSingleAnswerUsesCompactQuestionAnswerFormat(t *testing.T) {
	var output strings.Builder
	validation := validationResult{Passed: true}
	printSingleAnswer(&output, completionResult{
		Content:          "Короткий ответ",
		CompletionTokens: 3,
		FinishReason:     "stop",
	}, &validation)

	got := output.String()
	if !strings.Contains(got, "Короткий ответ") || strings.Contains(got, "Метрики:") {
		t.Fatalf("compact answer is incomplete: %q", got)
	}
	if strings.Contains(got, "ОТВЕТ С ОГРАНИЧЕНИЯМИ") || strings.Contains(got, "Формат:") || strings.Contains(got, "====") {
		t.Fatalf("compact answer contains service header: %q", got)
	}
}
