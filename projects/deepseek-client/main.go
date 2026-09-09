package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/history"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/llm"
)

type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

type responseControl = agent.Control

type completionResult = agent.Completion

func main() {
	if handled, exitCode := handleCommand(os.Args[1:], os.Stdout, os.Stderr); handled {
		os.Exit(exitCode)
	}

	input := bufio.NewReader(os.Stdin)
	exitCode := run(input)
	if exitCode != 0 {
		waitForEnter(input, os.Stdout)
	}
	os.Exit(exitCode)
}

func run(input *bufio.Reader) int {
	config, err := resolveConfig(input, os.Stdin, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "не удалось загрузить конфигурацию: %v\n", err)
		return 1
	}

	configPath, err := defaultConfigPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	config.History = &history.JSON{Dir: filepath.Join(filepath.Dir(configPath), "conversations")}
	config.InitialMessages, err = config.History.Load(context.Background(), "default")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Не удалось восстановить историю: %v\n", err)
		return 1
	}
	if isInteractiveTerminal(os.Stdin, os.Stdout) {
		return runTUI(config, askDeepSeek)
	}
	return runInteractiveSession(input, os.Stdout, os.Stderr, config, askDeepSeek)
}

func handleCommand(args []string, output io.Writer, errorOutput io.Writer) (bool, int) {
	if len(args) == 0 {
		return false, 0
	}
	if len(args) == 1 {
		switch args[0] {
		case "--list-formats":
			printFormatCatalog(output)
			return true, 0
		case "--list-models":
			printModelCatalog(output)
			return true, 0
		}
	}

	fmt.Fprintf(errorOutput, "неизвестные аргументы: %s\n", strings.Join(args, " "))
	fmt.Fprintln(errorOutput, "Доступные команды: --list-formats, --list-models")
	return true, 2
}

func readPrompt(input *bufio.Reader) (string, error) {
	prompt, err := input.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}

	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", errors.New("запрос не может быть пустым")
	}

	return prompt, nil
}

func waitForEnter(input *bufio.Reader, output io.Writer) {
	fmt.Fprint(output, "\nНажмите Enter, чтобы закрыть программу...")
	_, _ = input.ReadString('\n')
}

var completionHTTPClient = &http.Client{Timeout: 30 * time.Second}

func askDeepSeek(ctx context.Context, token string, prompt string, settings requestSettings) (completionResult, error) {
	client := llm.New(completionHTTPClient, map[string]string{"": token})
	return client.Complete(ctx, agent.Target{Model: settings.Model, BaseURL: settings.BaseURL, SendThinkingDisabled: settings.SendThinkingDisabled}, prompt, settings)
}

func modelsURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	baseURL = strings.TrimSuffix(baseURL, "/chat/completions")
	return baseURL + "/models"
}

func fetchModels(ctx context.Context, token string, profile apiProfile) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL(profile.BaseURL), nil)
	if err != nil {
		return nil, fmt.Errorf("create models request: %w", err)
	}
	if token = strings.TrimSpace(token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send models request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read models response: %w", err)
	}
	var parsed modelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if parsed.Error != nil {
			return nil, fmt.Errorf("api error: status=%d type=%s message=%s", resp.StatusCode, parsed.Error.Type, parsed.Error.Message)
		}
		return nil, fmt.Errorf("api error: status=%d response=%s", resp.StatusCode, string(body))
	}
	models := make([]string, 0, len(parsed.Data))
	for _, model := range parsed.Data {
		if id := strings.TrimSpace(model.ID); id != "" {
			models = append(models, id)
		}
	}
	if len(models) == 0 {
		return nil, errors.New("api returned an empty model list")
	}
	sort.Strings(models)
	return models, nil
}

func printMetrics(output io.Writer, uncontrolled completionResult, controlled completionResult) {
	fmt.Fprintln(output, "\n========================================")
	fmt.Fprintln(output, "СРАВНЕНИЕ")
	fmt.Fprintln(output, "========================================")
	fmt.Fprintf(output, "Без ограничений: model=%s, %d слов, %d символов, %d токенов, %.1f ток/с, finish_reason=%s\n",
		resultModel(uncontrolled),
		wordCount(uncontrolled.Content), utf8.RuneCountInString(uncontrolled.Content),
		uncontrolled.CompletionTokens, tokensPerSecond(uncontrolled), uncontrolled.FinishReason)
	fmt.Fprintf(output, "С ограничениями: model=%s, %d слов, %d символов, %d токенов, %.1f ток/с, finish_reason=%s\n",
		resultModel(controlled),
		wordCount(controlled.Content), utf8.RuneCountInString(controlled.Content),
		controlled.CompletionTokens, tokensPerSecond(controlled), controlled.FinishReason)
}

func tokensPerSecond(result completionResult) float64 {
	if result.Duration <= 0 {
		return 0
	}
	return float64(result.CompletionTokens) / result.Duration.Seconds()
}

func resultModel(result completionResult) string {
	if result.Model == "" {
		return "unknown"
	}
	return result.Model
}

func wordCount(text string) int {
	return len(strings.Fields(text))
}

func formatAnswer(answer string) string {
	normalized := strings.ReplaceAll(answer, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	lines := strings.Split(strings.TrimSpace(normalized), "\n")
	result := make([]string, 0, len(lines))
	previousLineWasEmpty := false

	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		isEmpty := strings.TrimSpace(line) == ""
		if isEmpty && previousLineWasEmpty {
			continue
		}

		result = append(result, line)
		previousLineWasEmpty = isEmpty
	}

	return strings.Join(result, "\n")
}

type chatRequest = llm.ChatRequest

func buildChatRequest(prompt string, settings requestSettings) chatRequest {
	return llm.BuildChatRequest(prompt, settings)
}
func chatCompletionsURL(baseURL string) string { return llm.ChatCompletionsURL(baseURL) }
