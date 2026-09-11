package main

import (
	"context"
	"fmt"
	"io"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

func printConversationStatus(output io.Writer, state sessionState) {
	id := conversationID(state.ConversationID)
	fmt.Fprintf(output, "Диалог: %s\n", id)
	store, ok := state.History.(agent.UsageStore)
	if !ok {
		fmt.Fprintln(output, "Суммарная статистика недоступна для этого хранилища")
		return
	}
	u, err := store.Usage(context.Background(), id)
	if err != nil {
		fmt.Fprintf(output, "Не удалось загрузить статистику: %v\n", err)
		return
	}
	fmt.Fprintf(output, "Завершённых запросов: %d\n", u.Turns)
	fmt.Fprintf(output, "Сумма известных токенов API: вход=%d (кэш=%d), выход=%d, всего=%d\n", u.Input, u.CachedInput, u.Output, u.Total)
	fmt.Fprintf(output, "Сумма известных оценок стоимости: $%.8f USD\n", u.CostUSD)
	if u.UnknownUsageTurns > 0 {
		fmt.Fprintf(output, "Запросов без usage: %d; итог токенов неполный\n", u.UnknownUsageTurns)
	}
	if u.UnknownCostTurns > 0 {
		fmt.Fprintf(output, "Запросов без оценки стоимости: %d; итог стоимости неполный\n", u.UnknownCostTurns)
	}
	fmt.Fprintln(output, "Учтены сохранённые ответы free/controlled; бенчмарки и неудачные вызовы не включены. Стоимость зависит от справочника тарифов, это не счёт провайдера.")
}
