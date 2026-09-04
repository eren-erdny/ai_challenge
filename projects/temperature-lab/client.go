package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const deepSeekAPIURL = "https://api.deepseek.com/chat/completions"

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature float64   `json:"temperature"`
	Stream      bool      `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message      message `json:"message"`
		FinishReason string  `json:"finish_reason"`
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

type completion struct {
	Content          string
	FinishReason     string
	CompletionTokens int
}

type deepSeekClient struct {
	token      string
	httpClient *http.Client
}

func newDeepSeekClient(token string) *deepSeekClient {
	return &deepSeekClient{
		token:      token,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

func (client *deepSeekClient) complete(messages []message, temperature float64) (completion, error) {
	payload := chatRequest{
		Model:       "deepseek-chat",
		Messages:    messages,
		Temperature: temperature,
		Stream:      false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return completion{}, fmt.Errorf("encode request: %w", err)
	}

	request, err := http.NewRequest(http.MethodPost, deepSeekAPIURL, bytes.NewReader(body))
	if err != nil {
		return completion{}, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Content-Type", "application/json")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return completion{}, fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return completion{}, fmt.Errorf("read response: %w", err)
	}
	var parsed chatResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return completion{}, fmt.Errorf("decode response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if parsed.Error != nil {
			return completion{}, fmt.Errorf("deepseek api: status=%d type=%s message=%s",
				response.StatusCode, parsed.Error.Type, parsed.Error.Message)
		}
		return completion{}, fmt.Errorf("deepseek api: status=%d", response.StatusCode)
	}
	if len(parsed.Choices) == 0 {
		return completion{}, fmt.Errorf("deepseek api returned no choices")
	}

	return completion{
		Content:          parsed.Choices[0].Message.Content,
		FinishReason:     parsed.Choices[0].FinishReason,
		CompletionTokens: parsed.Usage.CompletionTokens,
	}, nil
}
