package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Thinking    *ThinkingMode `json:"thinking,omitempty"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Stop        []string      `json:"stop,omitempty"`
}
type ThinkingMode struct {
	Type string `json:"type"`
}
type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens          int `json:"prompt_tokens"`
		CompletionTokens      int `json:"completion_tokens"`
		TotalTokens           int `json:"total_tokens"`
		PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
		PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
		PromptTokensDetails   struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

type chatMessage = agent.Message

type Client struct {
	http        *http.Client
	credentials map[string]string
}

var _ agent.Client = (*Client)(nil)

// New captures credentials separately from public agent requests and results.
func New(httpClient *http.Client, credentials map[string]string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	keys := make(map[string]string, len(credentials))
	for profile, key := range credentials {
		keys[profile] = key
	}
	return &Client{http: httpClient, credentials: keys}
}
func (c *Client) Complete(ctx context.Context, target agent.Target, prompt string, settings agent.Settings) (agent.Completion, error) {
	token := c.credentials[target.Profile]
	settings.Model, settings.BaseURL, settings.SendThinkingDisabled = target.Model, target.BaseURL, target.SendThinkingDisabled
	safe := func(message string) string {
		if token != "" {
			return strings.ReplaceAll(message, token, "[redacted]")
		}
		return message
	}

	payload := BuildChatRequest(prompt, settings)
	body, err := json.Marshal(payload)
	if err != nil {
		return agent.Completion{}, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ChatCompletionsURL(settings.BaseURL), bytes.NewReader(body))
	if err != nil {
		return agent.Completion{}, fmt.Errorf("create request: %w", err)
	}

	if token = strings.TrimSpace(token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")

	startedAt := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return agent.Completion{}, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return agent.Completion{}, fmt.Errorf("read response: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return agent.Completion{}, fmt.Errorf("decode response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if parsed.Error != nil {
			return agent.Completion{}, fmt.Errorf("api error: status=%d type=%s message=%s", resp.StatusCode, safe(parsed.Error.Type), safe(parsed.Error.Message))
		}
		return agent.Completion{}, fmt.Errorf("api error: status=%d response=%s", resp.StatusCode, safe(string(respBody)))
	}

	if len(parsed.Choices) == 0 {
		return agent.Completion{}, fmt.Errorf("api returned no choices")
	}
	if strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return agent.Completion{}, fmt.Errorf(
			"api returned empty content: completion_tokens=%d finish_reason=%s",
			parsed.Usage.CompletionTokens,
			parsed.Choices[0].FinishReason,
		)
	}
	model := parsed.Model
	if model == "" {
		model = settings.Model
	}
	totalTokens := parsed.Usage.TotalTokens
	if totalTokens == 0 {
		totalTokens = parsed.Usage.PromptTokens + parsed.Usage.CompletionTokens
	}

	cachedInputTokens := parsed.Usage.PromptCacheHitTokens
	if cachedInputTokens == 0 {
		cachedInputTokens = parsed.Usage.PromptTokensDetails.CachedTokens
	}

	return agent.Completion{
		Content:           parsed.Choices[0].Message.Content,
		Model:             model,
		FinishReason:      parsed.Choices[0].FinishReason,
		PromptTokens:      parsed.Usage.PromptTokens,
		CachedInputTokens: cachedInputTokens,
		CompletionTokens:  parsed.Usage.CompletionTokens,
		TotalTokens:       totalTokens,
		Duration:          time.Since(startedAt),
	}, nil
}
func BuildChatRequest(prompt string, settings agent.Settings) ChatRequest {
	payload := ChatRequest{
		Model:       settings.Model,
		Messages:    []chatMessage{{Role: "user", Content: prompt}},
		Temperature: settings.Temperature,
		Stream:      false,
	}
	if settings.SendThinkingDisabled {
		payload.Thinking = &ThinkingMode{Type: "disabled"}
	}

	if settings.Control != nil {
		payload.MaxTokens = settings.Control.MaxTokens
		payload.Stop = settings.Control.Stop
	}
	payload.Messages = settings.Messages
	if len(payload.Messages) == 0 {
		payload.Messages = agent.PrepareMessages(prompt, settings)
	}
	return payload
}
func ChatCompletionsURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/chat/completions") {
		return baseURL
	}
	return baseURL + "/chat/completions"
}
