package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

type ValidationStatus string

const (
	ValidationPassed  ValidationStatus = "OK"
	ValidationFailed  ValidationStatus = "FAIL"
	ValidationSkipped ValidationStatus = "INFO"
)

type ValidationCheck struct {
	Name    string
	Status  ValidationStatus
	Details string
}

type ValidationResult struct {
	Passed bool
	Checks []ValidationCheck
}

func ValidateAnswer(answer Completion, control *Control) ValidationResult {
	if control == nil {
		return ValidationResult{
			Passed: true,
			Checks: []ValidationCheck{{
				Name:    "Ограничения",
				Status:  ValidationSkipped,
				Details: "контроль ответа отключён",
			}},
		}
	}

	checks := []ValidationCheck{
		validateWordLimit(answer.Content, control.MaxWords),
		validateTokenLimit(answer.CompletionTokens, control.MaxTokens),
		validateFormat(answer.Content, control.Format),
		validateFinishReason(answer.FinishReason, control.Stop),
	}

	passed := true
	for _, check := range checks {
		if check.Status == ValidationFailed {
			passed = false
			break
		}
	}

	return ValidationResult{Passed: passed, Checks: checks}
}

func validateWordLimit(answer string, maxWords int) ValidationCheck {
	actual := wordCount(answer)
	status := ValidationPassed
	if actual > maxWords {
		status = ValidationFailed
	}
	return ValidationCheck{
		Name:    "Лимит слов",
		Status:  status,
		Details: fmt.Sprintf("%d из %d", actual, maxWords),
	}
}

func validateTokenLimit(actual int, maximum int) ValidationCheck {
	if actual == 0 {
		return ValidationCheck{
			Name:    "Лимит токенов",
			Status:  ValidationSkipped,
			Details: "API не вернул статистику completion_tokens",
		}
	}

	status := ValidationPassed
	if actual > maximum {
		status = ValidationFailed
	}
	return ValidationCheck{
		Name:    "Лимит токенов",
		Status:  status,
		Details: fmt.Sprintf("%d из %d", actual, maximum),
	}
}

func validateFinishReason(reason string, stop []string) ValidationCheck {
	if len(stop) == 0 {
		return ValidationCheck{
			Name:    "Завершение",
			Status:  ValidationSkipped,
			Details: "stop sequence не настроена",
		}
	}

	status := ValidationPassed
	if reason != "stop" {
		status = ValidationFailed
	}
	details := fmt.Sprintf("finish_reason=%q", reason)
	if reason == "stop" {
		details += "; API не различает естественную остановку и конкретную stop sequence"
	}
	return ValidationCheck{Name: "Завершение", Status: status, Details: details}
}

func validateFormat(answer string, format string) ValidationCheck {
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
		return ValidationCheck{
			Name:    "Формат custom",
			Status:  ValidationSkipped,
			Details: "пользовательская инструкция не проверяется локально",
		}
	default:
		details = fmt.Sprintf("неизвестный формат %q", format)
	}

	status := ValidationPassed
	if !valid {
		status = ValidationFailed
	}
	return ValidationCheck{Name: "Формат " + format, Status: status, Details: details}
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

func wordCount(text string) int { return len(strings.Fields(text)) }
