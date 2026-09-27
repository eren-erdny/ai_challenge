package main

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

type tuiToolCall struct {
	name, arguments, result string
	err                     error
}

// A trace belongs to one request and never changes the result sent to the model.
type tuiToolTrace struct {
	agent.ToolExecutor
	calls []tuiToolCall
}

func (t *tuiToolTrace) Execute(ctx context.Context, name, arguments string) (string, error) {
	result, err := t.ToolExecutor.Execute(ctx, name, arguments)
	t.calls = append(t.calls, tuiToolCall{name: name, arguments: arguments, result: result, err: err})
	return result, err
}

func (t *tuiToolTrace) render(secret string) string {
	var out strings.Builder
	for _, call := range t.calls {
		fmt.Fprintf(&out, "[tool] %s\nArguments: %s\n", toolPreview(call.name, secret), toolPreview(call.arguments, secret))
		if call.err != nil {
			fmt.Fprintf(&out, "Error: %s\n", toolPreview(call.err.Error(), secret))
		} else {
			fmt.Fprintf(&out, "Result: %s\n", toolPreview(call.result, secret))
		}
	}
	return strings.TrimSpace(out.String())
}

func toolPreview(text, secret string) string {
	if secret != "" {
		text = strings.ReplaceAll(text, secret, "[redacted]")
	}
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text)
	runes := []rune(text)
	if len(runes) > 1600 {
		return string(runes[:1600]) + "... [preview truncated]"
	}
	return text
}
