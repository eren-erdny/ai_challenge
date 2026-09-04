package main

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

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
		Model:       "deepseek-v4-pro",
		Temperature: 1.2,
		Strategy:    strategyExperts,
	})
	if request.Temperature != 1.2 {
		t.Fatalf("temperature = %g, want 1.2", request.Temperature)
	}
	if request.Model != "deepseek-v4-pro" {
		t.Fatalf("model = %q, want deepseek-v4-pro", request.Model)
	}
	if request.Thinking.Type != "disabled" {
		t.Fatalf("thinking = %q, want disabled", request.Thinking.Type)
	}
	if len(request.Messages) != 2 || !strings.Contains(request.Messages[0].Content, "Аналитик") ||
		!strings.Contains(request.Messages[0].Content, "Инженер") || !strings.Contains(request.Messages[0].Content, "Критик") {
		t.Fatalf("expert strategy system message is incomplete: %#v", request.Messages)
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
	control, err := buildResponseControl(defaultAppConfig().ResponseControl)
	if err != nil {
		t.Fatalf("buildResponseControl() returned error: %v", err)
	}
	printSingleAnswer(&output, completionResult{
		Content:          "Короткий ответ",
		CompletionTokens: 3,
		FinishReason:     "stop",
	}, control)

	got := output.String()
	if !strings.Contains(got, "Короткий ответ") || !strings.Contains(got, "Метрики:") {
		t.Fatalf("compact answer is incomplete: %q", got)
	}
	if strings.Contains(got, "ОТВЕТ С ОГРАНИЧЕНИЯМИ") || strings.Contains(got, "Формат:") || strings.Contains(got, "====") {
		t.Fatalf("compact answer contains service header: %q", got)
	}
}
