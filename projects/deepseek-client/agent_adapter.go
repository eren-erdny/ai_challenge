package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

// Configuration and credentials are resolved at the application boundary.
func executeQuestion(ctx context.Context, token, prompt string, state sessionState, output, errorOutput io.Writer, ask askFunction) *requestStatus {
	target := agent.Target{Profile: state.ActiveProfile, BaseURL: state.API.BaseURL, Model: state.Model, SendThinkingDisabled: isDeepSeekEndpoint(state.API.BaseURL)}
	request := agent.Request{Prompt: prompt, Mode: state.Mode, Target: target, Temperature: state.Temperature, Strategy: state.Strategy, Control: state.Control}
	credentials := map[string]string{target.Profile: token}
	if state.Mode == modeModelBenchmark {
		targets, err := buildModelBenchmarkTargets(state, token)
		if err != nil {
			fmt.Fprintf(errorOutput, "не удалось запустить benchmark моделей: %v\n", err)
			return nil
		}
		for _, item := range targets {
			request.Targets = append(request.Targets, agent.Target{Profile: item.ProfileName, BaseURL: item.Profile.BaseURL, Model: item.Model, SendThinkingDisabled: isDeepSeekEndpoint(item.Profile.BaseURL)})
			credentials[item.ProfileName] = item.Token
		}
	}
	client := agent.ClientFunc(func(ctx context.Context, target agent.Target, prompt string, settings agent.Settings) (agent.Completion, error) {
		return ask(ctx, credentials[target.Profile], prompt, settings)
	})
	request.ConversationID = conversationID(state.ConversationID)
	runner := agent.NewWithHistory(client, state.History)
	result, err := runner.Run(ctx, request)
	renderAgentResult(output, result)
	if err != nil {
		message := err.Error()
		for _, secret := range credentials {
			if secret != "" {
				message = strings.ReplaceAll(message, secret, "[redacted]")
			}
		}
		fmt.Fprintf(errorOutput, "ошибка запроса: %s\n", message)
	}
	if last := result.Last(); last != nil {
		return &requestStatus{Profile: last.Target.Profile, BaseURL: last.Target.BaseURL, Result: last.Answer}
	}
	return nil
}

func renderAgentResult(output io.Writer, result agent.Result) {
	switch result.Mode {
	case agent.ModelBenchmark:
		printModelBenchmark(output, result.Responses)
	case agent.TemperatureBenchmark:
		if len(result.Responses) > 0 {
			responses := make([]temperatureBenchmarkResult, 0, len(result.Responses))
			for _, r := range result.Responses {
				responses = append(responses, temperatureBenchmarkResult{Temperature: r.Temperature, Answer: r.Answer})
			}
			printTemperatureBenchmark(output, responses)
		}
	case agent.Compare:
		for i, r := range result.Responses {
			label := "БЕЗ ОГРАНИЧЕНИЙ"
			if i == 1 {
				label = "С ОГРАНИЧЕНИЯМИ"
			}
			fmt.Fprintf(output, "\nОТВЕТ %d: %s\n", i+1, label)
			printAgentResponse(output, r)
			if r.Validation != nil {
				printValidation(output, *r.Validation)
			}
		}
		if len(result.Responses) == 2 {
			printMetrics(output, result.Responses[0].Answer, result.Responses[1].Answer)
		}
	default:
		for _, r := range result.Responses {
			printAgentResponse(output, r)
		}
	}
	if result.Analysis != nil {
		title := "ИТОГОВЫЙ АНАЛИЗ"
		if result.Mode == agent.TemperatureBenchmark {
			title = "АНАЛИЗ МОДЕЛИ"
		}
		fmt.Fprintln(output, "\n"+title)
		fmt.Fprintln(output, strings.Repeat("-", 40))
		printAgentResponse(output, *result.Analysis)
	}
}

func printAgentResponse(output io.Writer, response agent.Response) {
	printSingleAnswer(output, response.Answer, response.Validation)
}
