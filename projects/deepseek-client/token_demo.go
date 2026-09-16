package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

type demoHistory struct{ messages []agent.Message }

func (s *demoHistory) Load(context.Context, string) ([]agent.Message, error) {
	return append([]agent.Message(nil), s.messages...), nil
}
func (s *demoHistory) Save(_ context.Context, _ string, m []agent.Message) error {
	s.messages = append([]agent.Message(nil), m...)
	return nil
}

// No credentials, disk history or paid API calls are used by this demonstration.
func runTokenDemo(output io.Writer) int {
	fmt.Fprintln(output, "ДЕМО: искусственный провайдер, лимит 200 токенов; это не замер реальной модели.")
	fmt.Fprintln(output, "Учебный тариф: вход $1 / 1M, выход $2 / 1M; кэш не моделируется.")
	store := &demoHistory{}
	counter := agent.ApproximateCounter{}
	client := agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, s agent.Settings) (agent.Completion, error) {
		input := 3 + 4*len(s.Messages)
		for _, m := range s.Messages {
			input += counter.Count(m.Content)
		}
		if input+s.MaxOutputTokens > 200 {
			return agent.Completion{}, fmt.Errorf("simulated provider: context_length_exceeded (input=%d, limit=200)", input)
		}
		return agent.Completion{Content: "OK", PromptTokens: input, CompletionTokens: 1, UsageKnown: true, FinishReason: "stop"}, nil
	})
	runner := agent.NewWithHistory(client, store)
	request := agent.Request{ConversationID: "demo", Mode: agent.Free, Target: agent.Target{Model: "simulated", ContextWindow: 200, MaxOutputTokens: 8}, Prompt: strings.Repeat("word ", 24)}
	fmt.Fprintln(output, "Ход | предыдущая история~ | последнее сообщение~ | текущий запрос целиком | ответ | цена USD | накоплено USD")
	total := 0.0
	for turn := 1; turn <= 10; turn++ {
		result, err := runner.Run(context.Background(), request)
		if err != nil {
			fmt.Fprintf(output, "%d: ПЕРЕПОЛНЕНИЕ: %v\n", turn, err)
			before := len(store.messages)
			request.Target.ContextWindow = 0
			_, remoteErr := runner.Run(context.Background(), request)
			fmt.Fprintf(output, "Без локальной проверки: %v\n", remoteErr)
			fmt.Fprintf(output, "История после отказов: %d сообщений (до отказов %d). Ответа нет; расход не прибавлен, usage не получен.\n", len(store.messages), before)
			fmt.Fprintln(output, "Вывод: новый вопрос одинаков, но повторная отправка истории увеличивает вход и стоимость каждого хода. При переполнении запрос отклоняется; /new начинает чистый диалог.")
			return 0
		}
		r := result.Responses[0].Tokens
		cost := (float64(r.InputActual) + 2*float64(r.OutputActual)) / 1_000_000
		total += cost
		fmt.Fprintf(output, "%d | %d | %d | %d | %d | %.6f | %.6f\n", turn, r.HistoryEstimate, r.LastUserMessageEstimate, r.InputActual, r.OutputActual, cost, total)
	}
	return 1
}
