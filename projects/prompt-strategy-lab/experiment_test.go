package main

import (
	"io"
	"strings"
	"testing"
)

const (
	testTask      = "Даны монеты 1, 3 и 4. Наберите сумму 6 минимальным числом монет."
	testReference = "Минимум 2 монеты: 3 + 3."
)

func TestRunExperimentUsesSixCallsAndFourStrategies(t *testing.T) {
	var prompts []string
	fakeComplete := func(messages []message) (completion, error) {
		prompts = append(prompts, messages[len(messages)-1].Content)
		content := "Минимум 2 монеты: 3 + 3."
		if len(prompts) == 3 {
			content = "Самостоятельный промпт с задачей про монеты и проверкой оптимальности."
		}
		if len(prompts) == 6 {
			content = "Все ответы корректны; наиболее точные методы разделили первое место."
		}
		return completion{Content: content, CompletionTokens: 10, FinishReason: "stop"}, nil
	}

	results, judgement, err := runExperiment(io.Discard, testTask, testReference, fakeComplete)
	if err != nil {
		t.Fatalf("runExperiment() returned error: %v", err)
	}
	if len(prompts) != 6 {
		t.Fatalf("API calls = %d, want 6", len(prompts))
	}
	if len(results) != 4 {
		t.Fatalf("strategy results = %d, want 4", len(results))
	}
	if results[2].ExtraPrompt == "" || prompts[3] != results[2].ExtraPrompt {
		t.Fatalf("generated prompt was not reused: result=%q request=%q", results[2].ExtraPrompt, prompts[3])
	}
	if !strings.Contains(prompts[1], "Решай пошагово") {
		t.Fatalf("step-by-step prompt is missing instruction: %q", prompts[1])
	}
	if !strings.Contains(prompts[4], "Аналитик") || !strings.Contains(prompts[4], "Инженер") || !strings.Contains(prompts[4], "Критик") {
		t.Fatalf("expert prompt is incomplete: %q", prompts[4])
	}
	if !strings.Contains(prompts[5], testReference) || judgement.Content == "" {
		t.Fatalf("judge did not receive reference or return judgement")
	}
}

func TestJudgeSolvesTaskWhenReferenceIsMissing(t *testing.T) {
	prompt, err := buildJudgePrompt("Сколько будет 2 + 2?", "", nil)
	if err != nil {
		t.Fatalf("buildJudgePrompt() returned error: %v", err)
	}
	if !strings.Contains(prompt, "самостоятельно реши задачу") {
		t.Fatalf("judge prompt does not request an independent solution: %q", prompt)
	}
}

func TestDirectPromptContainsNoStrategyInstruction(t *testing.T) {
	var firstPrompt string
	calls := 0
	fakeComplete := func(messages []message) (completion, error) {
		calls++
		if calls == 1 {
			firstPrompt = messages[0].Content
		}
		content := "Минимум 2 монеты: 3 + 3."
		if calls == 3 {
			content = "Готовый промпт"
		}
		return completion{Content: content}, nil
	}

	_, _, err := runExperiment(io.Discard, testTask, testReference, fakeComplete)
	if err != nil {
		t.Fatalf("runExperiment() returned error: %v", err)
	}
	if firstPrompt != testTask {
		t.Fatalf("direct prompt = %q, want exact task", firstPrompt)
	}
}
