package agent

import (
	"context"
	"time"
)

type Mode string

const (
	Free                 Mode = "free"
	Controlled           Mode = "controlled"
	Compare              Mode = "compare"
	TemperatureBenchmark Mode = "temperature_benchmark"
	ModelBenchmark       Mode = "model_benchmark"
)

type Strategy string

const (
	Standard   Strategy = "standard"
	StepByStep Strategy = "step_by_step"
	Experts    Strategy = "experts"
)

// Target identifies a provider connection. Credentials belong to the injected client.
type Target struct {
	ContextWindow        int
	MaxOutputTokens      int
	Profile              string
	BaseURL              string
	Model                string
	SendThinkingDisabled bool
}

type Settings struct {
	Tools                []ToolDefinition
	MaxOutputTokens      int
	Model                string
	BaseURL              string
	SendThinkingDisabled bool
	Temperature          float64
	Strategy             Strategy
	Control              *Control
	Messages             []Message
}

type Message struct {
	ToolCalls   *[]ToolCall `json:"tool_calls,omitempty"`
	ToolCallID  string      `json:"tool_call_id,omitempty"`
	FileContext string      `json:"file_context,omitempty"`
	Role        string      `json:"role"`
	Content     string      `json:"content"`
}

// Client receives messages already prepared by the agent.
type Client interface {
	Complete(context.Context, Target, string, Settings) (Completion, error)
}

// HistoryStore persists completed turns, without system instructions or credentials.
type HistoryStore interface {
	Load(context.Context, string) ([]Message, error)
	Save(context.Context, string, []Message) error
}

type ClientFunc func(context.Context, Target, string, Settings) (Completion, error)

func (f ClientFunc) Complete(ctx context.Context, target Target, prompt string, settings Settings) (Completion, error) {
	return f(ctx, target, prompt, settings)
}

type Request struct {
	ConversationID string
	Prompt         string
	Mode           Mode
	Target         Target
	Temperature    float64
	Strategy       Strategy
	Control        ControlConfig
	Targets        []Target
}

type Response struct {
	FileContext string
	Tokens      TokenReport
	Target      Target
	Answer      Completion
	Temperature float64
	Control     *Control
	Validation  *ValidationResult
	CostUSD     *float64
}

type Result struct {
	Failed         *Response
	ConversationID string
	Mode           Mode
	Responses      []Response
	Analysis       *Response
}

func (r Result) Last() *Response {
	if r.Analysis != nil {
		return r.Analysis
	}
	if len(r.Responses) == 0 {
		return nil
	}
	return &r.Responses[len(r.Responses)-1]
}

type Completion struct {
	UsageIncomplete   bool
	ToolCalls         []ToolCall
	UsageKnown        bool
	Content           string
	Model             string
	FinishReason      string
	PromptTokens      int
	CachedInputTokens int
	CompletionTokens  int
	TotalTokens       int
	Duration          time.Duration
}
type Control struct {
	Format       string
	SystemPrompt string
	MaxWords     int
	MaxTokens    int
	Stop         []string
}
type ControlConfig struct {
	Enabled           bool     `json:"enabled"`
	Format            string   `json:"format"`
	CustomInstruction string   `json:"custom_instruction,omitempty"`
	MaxWords          int      `json:"max_words"`
	MaxTokens         int      `json:"max_tokens"`
	StopSequences     []string `json:"stop_sequences"`
}
