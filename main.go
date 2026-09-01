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
)

const deepSeekAPIURL = "https://api.deepseek.com/chat/completions"

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func main() {
	input := bufio.NewReader(os.Stdin)
	exitCode := run(input)
	waitForEnter(input, os.Stdout)
	os.Exit(exitCode)
}

func run(input *bufio.Reader) int {
	token, err := resolveToken(input, os.Stdin, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "не удалось получить токен DeepSeek: %v\n", err)
		return 1
	}

	fmt.Println("Добрый день! Спросите у меня!")
	fmt.Print("Ваш вопрос: ")

	prompt, err := readPrompt(input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "не удалось прочитать запрос: %v\n", err)
		return 1
	}

	fmt.Println("\nОбрабатываю запрос...")
	answer, err := askDeepSeek(token, prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ошибка запроса: %v\n", err)
		return 1
	}

	printAnswer(answer)
	return 0
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

func askDeepSeek(token string, prompt string) (string, error) {
	payload := chatRequest{
		Model: "deepseek-chat",
		Messages: []chatMessage{
			{
				Role: "system",
				Content: "Отвечай на русском языке в Markdown. Делай ответ удобным для чтения: " +
					"используй короткие абзацы, заголовки, списки и блоки кода только там, где они уместны. " +
					"Не добавляй лишнее вступление.",
			},
			{Role: "user", Content: prompt},
		},
		Stream: false,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, deepSeekAPIURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("decode response: %w; raw response: %s", err, string(respBody))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if parsed.Error != nil {
			return "", fmt.Errorf("deepseek api error: status=%d type=%s message=%s", resp.StatusCode, parsed.Error.Type, parsed.Error.Message)
		}
		return "", fmt.Errorf("deepseek api error: status=%d response=%s", resp.StatusCode, string(respBody))
	}

	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("deepseek api returned no choices")
	}

	return parsed.Choices[0].Message.Content, nil
}

func printAnswer(answer string) {
	fmt.Println("\nОтвет DeepSeek")
	fmt.Println("--------------")
	fmt.Println(formatAnswer(answer))
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
