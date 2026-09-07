package main

import (
	"io"
	"strings"
	"testing"
)

func TestRunExperimentUsesSameQueryAndRequiredTemperatures(t *testing.T) {
	const query = "Объясни, почему небо голубое."
	var calls []struct {
		prompt      string
		temperature float64
	}
	fakeComplete := func(messages []message, temperature float64) (completion, error) {
		calls = append(calls, struct {
			prompt      string
			temperature float64
		}{messages[len(messages)-1].Content, temperature})
		return completion{Content: "Ответ", FinishReason: "stop", CompletionTokens: 5}, nil
	}

	results, judgement, err := runExperiment(io.Discard, query, fakeComplete)
	if err != nil {
		t.Fatalf("runExperiment() returned error: %v", err)
	}
	if len(calls) != 4 {
		t.Fatalf("API calls = %d, want 4", len(calls))
	}
	if len(results) != 3 || judgement.Content == "" {
		t.Fatalf("results = %d, judgement empty = %v", len(results), judgement.Content == "")
	}

	wantTemperatures := []float64{0, 0.7, 1.2}
	for index, want := range wantTemperatures {
		if calls[index].temperature != want {
			t.Fatalf("call %d temperature = %g, want %g", index+1, calls[index].temperature, want)
		}
		if calls[index].prompt != query {
			t.Fatalf("call %d prompt changed: %q", index+1, calls[index].prompt)
		}
	}
	if calls[3].temperature != 0 {
		t.Fatalf("judge temperature = %g, want 0", calls[3].temperature)
	}
	for _, criterion := range []string{"точность", "креативность", "разнообразие"} {
		if !strings.Contains(calls[3].prompt, criterion) {
			t.Fatalf("judge prompt is missing %q", criterion)
		}
	}
}

func TestBuildJudgePromptContainsAllAnswers(t *testing.T) {
	results := []temperatureResult{
		{Temperature: 0, Answer: completion{Content: "zero"}},
		{Temperature: 0.7, Answer: completion{Content: "balanced"}},
		{Temperature: 1.2, Answer: completion{Content: "creative"}},
	}
	prompt, err := buildJudgePrompt("test query", results)
	if err != nil {
		t.Fatalf("buildJudgePrompt() returned error: %v", err)
	}
	for _, value := range []string{"test query", "zero", "balanced", "creative"} {
		if !strings.Contains(prompt, value) {
			t.Fatalf("judge prompt is missing %q", value)
		}
	}
}
