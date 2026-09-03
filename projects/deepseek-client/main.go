package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

const deepSeekAPIURL = "https://api.deepseek.com/chat/completions"

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	Stream    bool          `json:"stream"`
	MaxTokens int           `json:"max_tokens,omitempty"`
	Stop      []string      `json:"stop,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

type responseControl struct {
	Format       string
	SystemPrompt string
	MaxWords     int
	MaxTokens    int
	Stop         []string
}

type completionResult struct {
	Content          string
	FinishReason     string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

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

	return runInteractiveSession(input, os.Stdout, os.Stderr, config, askDeepSeek)
}

func handleCommand(args []string, output io.Writer, errorOutput io.Writer) (bool, int) {
	if len(args) == 0 {
		return false, 0
	}
	if len(args) == 1 && args[0] == "--list-formats" {
		printFormatCatalog(output)
		return true, 0
	}

	fmt.Fprintf(errorOutput, "неизвестные аргументы: %s\n", strings.Join(args, " "))
	fmt.Fprintln(errorOutput, "Доступная команда: --list-formats")
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

func askDeepSeek(token string, prompt string, control *responseControl) (completionResult, error) {
	payload := buildChatRequest(prompt, control)
	body, err := json.Marshal(payload)
	if err != nil {
		return completionResult{}, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, deepSeekAPIURL, bytes.NewReader(body))
	if err != nil {
		return completionResult{}, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return completionResult{}, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return completionResult{}, fmt.Errorf("read response: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return completionResult{}, fmt.Errorf("decode response: %w; raw response: %s", err, string(respBody))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if parsed.Error != nil {
			return completionResult{}, fmt.Errorf("deepseek api error: status=%d type=%s message=%s", resp.StatusCode, parsed.Error.Type, parsed.Error.Message)
		}
		return completionResult{}, fmt.Errorf("deepseek api error: status=%d response=%s", resp.StatusCode, string(respBody))
	}

	if len(parsed.Choices) == 0 {
		return completionResult{}, fmt.Errorf("deepseek api returned no choices")
	}

	return completionResult{
		Content:          parsed.Choices[0].Message.Content,
		FinishReason:     parsed.Choices[0].FinishReason,
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		TotalTokens:      parsed.Usage.TotalTokens,
	}, nil
}

func buildChatRequest(prompt string, control *responseControl) chatRequest {
	payload := chatRequest{
		Model:    "deepseek-chat",
		Messages: []chatMessage{{Role: "user", Content: prompt}},
		Stream:   false,
	}

	if control != nil {
		payload.Messages = append([]chatMessage{{Role: "system", Content: control.SystemPrompt}}, payload.Messages...)
		payload.MaxTokens = control.MaxTokens
		payload.Stop = control.Stop
	}

	return payload
}

func printComparison(output io.Writer, uncontrolled completionResult, controlled completionResult, control *responseControl) {
	fmt.Fprintln(output, "\n========================================")
	fmt.Fprintln(output, "ОТВЕТ 1: БЕЗ ОГРАНИЧЕНИЙ")
	fmt.Fprintln(output, "========================================")
	fmt.Fprintln(output, formatAnswer(uncontrolled.Content))

	fmt.Fprintln(output, "\n========================================")
	if control == nil {
		fmt.Fprintln(output, "ОТВЕТ 2: КОНТРОЛЬ ОТКЛЮЧЁН")
	} else {
		fmt.Fprintln(output, "ОТВЕТ 2: С ОГРАНИЧЕНИЯМИ")
		fmt.Fprintf(output, "Формат: %s\n", control.Format)
		fmt.Fprintf(output, "Лимит: не более %d слов, max_tokens=%d\n", control.MaxWords, control.MaxTokens)
		fmt.Fprintf(output, "Условия завершения: stop=%q\n", control.Stop)
	}
	fmt.Fprintln(output, "========================================")
	fmt.Fprintln(output, formatAnswer(controlled.Content))

	printValidation(output, validateAnswer(controlled, control))
	printMetrics(output, uncontrolled, controlled)
}

func printMetrics(output io.Writer, uncontrolled completionResult, controlled completionResult) {
	fmt.Fprintln(output, "\n========================================")
	fmt.Fprintln(output, "СРАВНЕНИЕ")
	fmt.Fprintln(output, "========================================")
	fmt.Fprintf(output, "Без ограничений: %d слов, %d символов, %d токенов, finish_reason=%s\n",
		wordCount(uncontrolled.Content), utf8.RuneCountInString(uncontrolled.Content),
		uncontrolled.CompletionTokens, uncontrolled.FinishReason)
	fmt.Fprintf(output, "С ограничениями: %d слов, %d символов, %d токенов, finish_reason=%s\n",
		wordCount(controlled.Content), utf8.RuneCountInString(controlled.Content),
		controlled.CompletionTokens, controlled.FinishReason)
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
