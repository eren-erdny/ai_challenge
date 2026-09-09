package main

import (
	"fmt"
	"io"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

type validationResult = agent.ValidationResult
type validationStatus = agent.ValidationStatus
type validationCheck = agent.ValidationCheck

const (
	validationPassed  = agent.ValidationPassed
	validationFailed  = agent.ValidationFailed
	validationSkipped = agent.ValidationSkipped
)

func validateAnswer(answer completionResult, control *responseControl) validationResult {
	return agent.ValidateAnswer(answer, control)
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
