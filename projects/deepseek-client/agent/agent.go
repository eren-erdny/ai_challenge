package agent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// Agent owns request preparation, scenario execution and output validation.
// History is optional and supplied independently of the transport and UI.
type Agent struct {
	tools   ToolExecutor
	counter TokenCounter
	client  Client
	history HistoryStore
}

func New(client Client) *Agent { return &Agent{client: client} }

func NewWithHistory(client Client, history HistoryStore) *Agent {
	return &Agent{client: client, history: history}
}

type invocation struct {
	allowTools  bool
	target      Target
	temperature float64
	strategy    Strategy
	control     *Control
	validate    bool
	messages    []Message
}

// Run returns completed responses even when a later model or judge fails.
func (a *Agent) Run(ctx context.Context, request Request) (Result, error) {
	result := Result{ConversationID: request.ConversationID, Mode: request.Mode}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if a == nil || a.client == nil {
		return result, errors.New("LLM client is required")
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return result, errors.New("request must not be empty")
	}
	if math.IsNaN(request.Temperature) || math.IsInf(request.Temperature, 0) || request.Temperature < 0 || request.Temperature > 2 {
		return result, errors.New("temperature must be between 0 and 2")
	}
	if request.Strategy == "" {
		request.Strategy = Standard
	}
	switch request.Strategy {
	case Standard, StepByStep, Experts:
	default:
		return result, fmt.Errorf("unknown strategy %q", request.Strategy)
	}
	if request.Mode == "" {
		request.Mode = Free
		result.Mode = Free
	}
	// Copy mutable input before invoking dependencies.
	request.Control.StopSequences = append([]string(nil), request.Control.StopSequences...)
	request.Targets = append([]Target(nil), request.Targets...)
	plan, judge, err := makePlan(request)
	if err != nil {
		return result, err
	}
	remember := a.history != nil && (request.Mode == Free || request.Mode == Controlled)
	var history []Message
	if remember {
		if strings.TrimSpace(request.ConversationID) == "" {
			return result, errors.New("conversation ID is required for history")
		}
		history, err = a.history.Load(ctx, request.ConversationID)
		if err != nil {
			return result, fmt.Errorf("load conversation: %w", err)
		}
		plan[0].messages = append([]Message(nil), history...)
	}
	for _, step := range plan {
		response, err := a.invoke(ctx, request.Prompt, step)
		if err != nil {
			result.Failed = &response
			return result, fmt.Errorf("model %s: %w", step.target.Model, err)
		}
		result.Responses = append(result.Responses, response)
	}
	if remember {
		history = append(history, Message{Role: "user", Content: request.Prompt, FileContext: result.Responses[0].FileContext}, Message{Role: "assistant", Content: result.Responses[0].Answer.Content})
		var saveErr error
		if store, ok := a.history.(TurnStore); ok {
			r := result.Responses[0]
			saveErr = store.SaveTurn(ctx, request.ConversationID, history, TurnUsage{Model: r.Answer.Model, UsageKnown: r.Tokens.UsageKnown, Input: r.Answer.PromptTokens, CachedInput: r.Answer.CachedInputTokens, Output: r.Answer.CompletionTokens, Total: r.Answer.TotalTokens, CostUSD: r.CostUSD, Duration: r.Answer.Duration})
		} else {
			saveErr = a.history.Save(ctx, request.ConversationID, history)
		}
		if err := saveErr; err != nil {
			return result, fmt.Errorf("answer received but conversation was not saved: %w", err)
		}
	}
	if judge != nil {
		analysis, err := a.invoke(ctx, analysisPrompt(request, result.Responses), *judge)
		if err != nil {
			result.Failed = &analysis
			return result, fmt.Errorf("judge %s: %w", judge.target.Model, err)
		}
		result.Analysis = &analysis
	}
	return result, nil
}

func makePlan(request Request) ([]invocation, *invocation, error) {
	var control *Control
	var err error
	switch request.Mode {
	case Controlled, Compare:
		request.Control.Enabled = true
		control, err = BuildControl(request.Control)
	case ModelBenchmark:
		control, err = BuildControl(request.Control)
	case Free, TemperatureBenchmark:
	default:
		return nil, nil, fmt.Errorf("unknown mode %q", request.Mode)
	}
	if err != nil {
		return nil, nil, err
	}
	base := invocation{target: request.Target, temperature: request.Temperature, strategy: request.Strategy, control: control, validate: control != nil}
	var plan []invocation
	var judge *invocation
	switch request.Mode {
	case Free, Controlled:
		base.allowTools = true
		plan = append(plan, base)
	case Compare:
		free := base
		free.control = nil
		free.validate = false
		plan = append(plan, free, base)
	case TemperatureBenchmark:
		base.control = temperatureBenchmarkControl()
		base.validate = false
		for _, temperature := range []float64{0, 1.2, 2} {
			step := base
			step.temperature = temperature
			plan = append(plan, step)
		}
		judge = &invocation{target: request.Target, temperature: 0, strategy: Standard, control: temperatureAnalysisControl()}
	case ModelBenchmark:
		if len(request.Targets) != 3 {
			return nil, nil, errors.New("model benchmark requires three targets")
		}
		for _, target := range request.Targets {
			step := base
			step.target = target
			plan = append(plan, step)
		}
		judge = &invocation{target: request.Targets[1], temperature: 0, strategy: Standard, control: modelBenchmarkAnalysisControl()}
	}
	for _, step := range plan {
		if step.target.ContextWindow < 0 || step.target.MaxOutputTokens < 0 {
			return nil, nil, errors.New("token limits must not be negative")
		}
		if strings.TrimSpace(step.target.Model) == "" {
			return nil, nil, errors.New("model is required")
		}
	}
	return plan, judge, nil
}

func cloneControl(control *Control) *Control {
	if control == nil {
		return nil
	}
	copy := *control
	copy.Stop = append([]string(nil), control.Stop...)
	return &copy
}

func (a *Agent) invoke(ctx context.Context, prompt string, step invocation) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	settings := Settings{Model: step.target.Model, BaseURL: step.target.BaseURL, SendThinkingDisabled: step.target.SendThinkingDisabled,
		Temperature: step.temperature, Strategy: step.strategy, Control: cloneControl(step.control)}
	settings.Messages = PrepareMessages(prompt, settings)
	// Current policies precede history; the new user message always comes last.
	last := len(settings.Messages) - 1
	settings.Messages = append(settings.Messages[:last], append(append([]Message(nil), step.messages...), settings.Messages[last])...)
	for i := range settings.Messages {
		settings.Messages[i].Content += settings.Messages[i].FileContext
		settings.Messages[i].FileContext = ""
	}
	settings.MaxOutputTokens = step.target.MaxOutputTokens
	if step.control != nil && step.control.MaxTokens > 0 {
		settings.MaxOutputTokens = step.control.MaxTokens
	}
	counter := a.counter
	if counter == nil {
		counter = ApproximateCounter{}
	}
	report := tokenReport(counter, prompt, step.messages, settings.Messages, step.target, settings.MaxOutputTokens)
	response := Response{Target: step.target, Temperature: step.temperature, Control: cloneControl(step.control), Tokens: report}
	if report.ContextWindow > 0 && (report.InputEstimate > report.ContextWindow || report.OutputReserve > report.ContextWindow-report.InputEstimate) {
		return response, &ContextLimitError{Tokens: report}
	}
	started := time.Now()
	answer, fileContext, err := a.completeWithTools(ctx, step, prompt, settings, counter)
	if err != nil {
		return response, err
	}
	if err := ctx.Err(); err != nil {
		return response, err
	}
	if strings.TrimSpace(answer.Content) == "" {
		return Response{}, errors.New("LLM returned empty content")
	}
	if answer.Model == "" {
		answer.Model = step.target.Model
	}
	if answer.TotalTokens == 0 {
		answer.TotalTokens = answer.PromptTokens + answer.CompletionTokens
	}
	if answer.Duration <= 0 {
		answer.Duration = time.Since(started)
	}
	response.Answer = answer
	response.FileContext = fileContext
	response.Tokens.AnswerEstimate = counter.Count(answer.Content)
	response.Tokens.UsageKnown = !answer.UsageIncomplete && (answer.UsageKnown || answer.PromptTokens > 0 || answer.CompletionTokens > 0)
	response.Tokens.InputActual = answer.PromptTokens
	response.Tokens.OutputActual = answer.CompletionTokens
	response.Tokens.TotalActual = answer.TotalTokens
	if step.validate {
		validation := ValidateAnswer(answer, step.control)
		response.Validation = &validation
	}
	if cost, known := EstimateCost(step.target.BaseURL, answer); known && response.Tokens.UsageKnown {
		response.CostUSD = &cost
	}
	return response, nil
}
