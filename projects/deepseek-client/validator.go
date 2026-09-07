package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type validationStatus string

const (
	validationPassed  validationStatus = "OK"
	validationFailed  validationStatus = "FAIL"
	validationSkipped validationStatus = "INFO"
)

type validationCheck struct {
	Name    string
	Status  validationStatus
	Details string
}

type validationResult struct {
	Passed bool
	Checks []validationCheck
}

func validateAnswer(answer completionResult, control *responseControl) validationResult {
	if control == nil {
		return validationResult{
			Passed: true,
			Checks: []validationCheck{{
				Name:    "Ограничения",
				Status:  validationSkipped,
				Details: "контроль ответа отключён",
			}},
		}
	}

	checks := []validationCheck{
		validateWordLimit(answer.Content, control.MaxWords),
		validateTokenLimit(answer.CompletionTokens, control.MaxTokens),
		validateFormat(answer.Content, control.Format),
		validateFinishReason(answer.FinishReason, control.Stop),
	}

	passed := true
	for _, check := range checks {
		if check.Status == validationFailed {
			passed = false
			break
		}
	}

	return validationResult{Passed: passed, Checks: checks}
}

func validateWordLimit(answer string, maxWords int) validationCheck {
	actual := wordCount(answer)
	status := validationPassed
	if actual > maxWords {
		status = validationFailed
	}
	return validationCheck{
		Name:    "Лимит слов",
		Status:  status,
		Details: fmt.Sprintf("%d из %d", actual, maxWords),
	}
}

func validateTokenLimit(actual int, maximum int) validationCheck {
	if actual == 0 {
		return validationCheck{
			Name:    "Лимит токенов",
			Status:  validationSkipped,
			Details: "API не вернул статистику completion_tokens",
		}
	}

	status := validationPassed
	if actual > maximum {
		status = validationFailed
	}
	return validationCheck{
		Name:    "Лимит токенов",
		Status:  status,
		Details: fmt.Sprintf("%d из %d", actual, maximum),
	}
}

func validateFinishReason(reason string, stop []string) validationCheck {
	if len(stop) == 0 {
		return validationCheck{
			Name:    "Завершение",
			Status:  validationSkipped,
			Details: "stop sequence не настроена",
		}
	}

	status := validationPassed
	if reason != "stop" {
		status = validationFailed
	}
	details := fmt.Sprintf("finish_reason=%q", reason)
	if reason == "stop" {
		details += "; API не различает естественную остановку и конкретную stop sequence"
	}
	return validationCheck{Name: "Завершение", Status: status, Details: details}
}

func validateFormat(answer string, format string) validationCheck {
	valid := false
	details := ""

	switch format {
	case "plain_text":
		valid = strings.TrimSpace(answer) != ""
		details = "ответ содержит текст"
	case "short_answer":
		paragraphs := paragraphCount(answer)
		valid = paragraphs == 1
		details = fmt.Sprintf("абзацев: %d, ожидался 1", paragraphs)
	case "bullet_list":
		items, onlyList := bulletListStats(answer)
		valid = onlyList && items > 0
		details = fmt.Sprintf("пунктов: %d; посторонний текст: %t", items, !onlyList)
	case "structured_markdown":
		items, _ := bulletListStats(answer)
		hasSummary := strings.Contains(answer, "## Краткий ответ")
		hasPoints := strings.Contains(answer, "## Ключевые пункты")
		valid = hasSummary && hasPoints && items == 3
		details = fmt.Sprintf("заголовки: %t; пунктов: %d из 3", hasSummary && hasPoints, items)
	case "json":
		valid, details = validateJSONFormat(answer)
	case "custom":
		return validationCheck{
			Name:    "Формат custom",
			Status:  validationSkipped,
			Details: "пользовательская инструкция не проверяется локально",
		}
	default:
		details = fmt.Sprintf("неизвестный формат %q", format)
	}

	status := validationPassed
	if !valid {
		status = validationFailed
	}
	return validationCheck{Name: "Формат " + format, Status: status, Details: details}
}

func validateJSONFormat(answer string) (bool, string) {
	var value struct {
		Summary string   `json:"summary"`
		Points  []string `json:"points"`
	}
	if err := json.Unmarshal([]byte(answer), &value); err != nil {
		return false, "некорректный JSON: " + err.Error()
	}
	if strings.TrimSpace(value.Summary) == "" {
		return false, "поле summary отсутствует или пустое"
	}
	if len(value.Points) == 0 {
		return false, "массив points отсутствует или пуст"
	}
	return true, fmt.Sprintf("summary заполнен; пунктов: %d", len(value.Points))
}

func paragraphCount(text string) int {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	count := 0
	inParagraph := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			inParagraph = false
			continue
		}
		if !inParagraph {
			count++
			inParagraph = true
		}
	}
	return count
}

func bulletListStats(text string) (items int, onlyList bool) {
	onlyList = true
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "## ") {
			continue
		}
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			items++
			continue
		}
		onlyList = false
	}
	return items, onlyList
}

func printValidation(output io.Writer, result validationResult) {
	fmt.Fprintln(output, "\n========================================")
	fmt.Fprintln(output, "ЛОКАЛЬНЫЙ СУДЬЯ")
	fmt.Fprintln(output, "========================================")
	for _, check := range result.Checks {
		fmt.Fprintf(output, "[%s] %s: %s\n", check.Status, check.Name, check.Details)
	}
	if result.Passed {
		fmt.Fprintln(output, "Итог: ответ соответствует проверяемым ограничениям")
	} else {
		fmt.Fprintln(output, "Итог: ответ не соответствует всем ограничениям")
	}
}
