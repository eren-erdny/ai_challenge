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
	target.ContextWindow, target.MaxOutputTokens = state.API.ContextWindow, state.API.MaxOutputTokens
	request := agent.Request{Prompt: prompt, Mode: state.Mode, Target: target, Temperature: state.Temperature, Strategy: state.Strategy, Control: state.Control}
	credentials := map[string]string{target.Profile: token}
	if state.Mode == modeModelBenchmark && prompt != "/compress" {
		targets, err := buildModelBenchmarkTargets(state, token)
		if err != nil {
			fmt.Fprintf(errorOutput, "не удалось запустить benchmark моделей: %v\n", err)
			return nil
		}
		for _, item := range targets {
			request.Targets = append(request.Targets, agent.Target{Profile: item.ProfileName, BaseURL: item.Profile.BaseURL, Model: item.Model, SendThinkingDisabled: isDeepSeekEndpoint(item.Profile.BaseURL), ContextWindow: item.Profile.ContextWindow, MaxOutputTokens: item.Profile.MaxOutputTokens})
			credentials[item.ProfileName] = item.Token
		}
	}
	client := agent.ClientFunc(func(ctx context.Context, target agent.Target, prompt string, settings agent.Settings) (agent.Completion, error) {
		return ask(ctx, credentials[target.Profile], prompt, settings)
	})
	request.ConversationID = conversationID(state.ConversationID)
	request.Compression = state.Compression
	runner := agent.NewWithHistory(client, state.History)
	if !state.ToolsDisabled {
		runner = runner.WithTools(state.Tools)
	}
	var result agent.Result
	var err error
	if prompt == "/compress" {
		compressed, compressErr := runner.Compress(ctx, request.ConversationID, target, state.Compression)
		result.Compression = &compressed
		err = compressErr
		if compressErr == nil && state.Compression.Memory() != agent.MemorySummary {
			fmt.Fprintf(output, "Summary сохранено, но текущая стратегия %s его не отправляет. Переключение: /memory summary.\n", state.Compression.Memory())
		}
	} else {
		result, err = runner.Run(ctx, request)
	}
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
		return &requestStatus{Profile: last.Target.Profile, BaseURL: last.Target.BaseURL, Result: last.Answer, Tokens: &last.Tokens}
	}
	return nil
}

func renderAgentResult(output io.Writer, result agent.Result) {
	if c := result.Compression; c != nil {
		fmt.Fprintf(output, "Сжатие: применено=%t; оценка истории до=%d, после=%d токенов; API-вызовов=%d (без usage=%d), сообщённый вход=%d, выход=%d. Полный учёт: /status.\n", c.Changed, c.Before, c.After, c.Calls, c.UnknownUsageCalls, c.Input, c.Output)
	}
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
	if response.Answer.FinishReason == "length" {
		fmt.Fprintln(output, "Внимание: ответ обрезан лимитом генерации или контекста (finish_reason=length).")
	}
}

func printTokenReport(output io.Writer, r agent.TokenReport) {
	fmt.Fprintf(output, "Токены (оценка): последнее сообщение пользователя=%d, предыдущая история=%d (%d сообщений), инструкции=%d, служебные=%d, текущий запрос целиком=%d\n", r.LastUserMessageEstimate, r.HistoryEstimate, r.HistoryMessages, r.SystemEstimate, r.FramingEstimate, r.InputEstimate)
	if r.UsageKnown {
		fmt.Fprintf(output, "Токены API: вход=%d, ответ=%d, всего=%d\n", r.InputActual, r.OutputActual, r.TotalActual)
		fmt.Fprintln(output, "Вход (prompt_tokens) = текущий запрос целиком: system + история + последнее сообщение пользователя + служебное оформление.")
	} else {
		fmt.Fprintf(output, "Usage API недоступен; ответ (оценка)=%d\n", r.AnswerEstimate)
	}
	if r.ContextWindow > 0 {
		fmt.Fprintf(output, "Контекст (оценка): вход %d + резерв ответа %d / лимит %d\n", r.InputEstimate, r.OutputReserve, r.ContextWindow)
	}
}
