package main

import (
	"strings"
	"testing"
)

func TestValidateStructuredMarkdownPasses(t *testing.T) {
	control := &responseControl{
		Format:    "structured_markdown",
		MaxWords:  80,
		MaxTokens: 200,
		Stop:      []string{"<END>"},
	}
	answer := completionResult{
		Content: "## Краткий ответ\n\nКороткий текст.\n\n" +
			"## Ключевые пункты\n\n- Первый\n- Второй\n- Третий",
		CompletionTokens: 40,
		FinishReason:     "stop",
	}

	result := validateAnswer(answer, control)

	if !result.Passed {
		t.Fatalf("validateAnswer() = %#v, want passed", result)
	}
}

func TestValidateStructuredMarkdownReportsFailures(t *testing.T) {
	control := &responseControl{
		Format:    "structured_markdown",
		MaxWords:  2,
		MaxTokens: 20,
		Stop:      []string{"<END>"},
	}
	answer := completionResult{
		Content:          "Ответ содержит слишком много слов",
		CompletionTokens: 20,
		FinishReason:     "length",
	}

	result := validateAnswer(answer, control)

	if result.Passed {
		t.Fatalf("validateAnswer() = %#v, want failed", result)
	}
	if got := countChecks(result, validationFailed); got != 3 {
		t.Fatalf("failed checks = %d, want 3: %#v", got, result)
	}
}

func TestValidateJSONFormat(t *testing.T) {
	valid := completionResult{Content: `{"summary":"Go","points":["Простой","Быстрый"]}`, CompletionTokens: 20, FinishReason: "stop"}
	invalid := completionResult{Content: `{"summary":"Go"}`, CompletionTokens: 20, FinishReason: "stop"}
	control := &responseControl{Format: "json", MaxWords: 80, MaxTokens: 200}

	if result := validateAnswer(valid, control); !result.Passed {
		t.Fatalf("valid JSON failed validation: %#v", result)
	}
	if result := validateAnswer(invalid, control); result.Passed {
		t.Fatalf("invalid JSON passed validation: %#v", result)
	}
}

func TestCustomFormatIsInformational(t *testing.T) {
	control := &responseControl{Format: "custom", MaxWords: 80, MaxTokens: 200}
	answer := completionResult{Content: "Ответ", CompletionTokens: 5, FinishReason: "stop"}

	result := validateAnswer(answer, control)

	if !result.Passed || countChecks(result, validationSkipped) == 0 {
		t.Fatalf("custom validation = %#v, want passed with informational check", result)
	}
}

func TestPrintValidation(t *testing.T) {
	var output strings.Builder
	printValidation(&output, validationResult{
		Passed: true,
		Checks: []validationCheck{{Name: "Лимит слов", Status: validationPassed, Details: "10 из 80"}},
	})

	if !strings.Contains(output.String(), "[OK] Лимит слов") || !strings.Contains(output.String(), "Итог:") {
		t.Fatalf("unexpected validation output: %q", output.String())
	}
}

func countChecks(result validationResult, status validationStatus) int {
	count := 0
	for _, check := range result.Checks {
		if check.Status == status {
			count++
		}
	}
	return count
}
