package agent

import (
	"fmt"
	"unicode/utf8"
)

// TokenCounter can be replaced with a tokenizer for a particular model.
type TokenCounter interface{ Count(string) int }

// ApproximateCounter is a heuristic, NOT a model tokenizer or billing counter.
// ASCII: approximately 4 characters/token; non-ASCII: approximately 1 rune/token.
type ApproximateCounter struct{}

func (ApproximateCounter) Count(text string) int {
	ascii, other := 0, 0
	for _, r := range text {
		if r < utf8.RuneSelf {
			ascii++
		} else {
			other++
		}
	}
	return (ascii+3)/4 + other
}

type TokenReport struct {
	LastUserMessageEstimate int
	HistoryEstimate         int
	SystemEstimate          int
	FramingEstimate         int
	InputEstimate           int
	AnswerEstimate          int
	HistoryMessages         int
	ContextWindow           int
	OutputReserve           int
	InputActual             int
	OutputActual            int
	TotalActual             int
	UsageKnown              bool
}

type ContextLimitError struct{ Tokens TokenReport }

func (e *ContextLimitError) Error() string {
	return fmt.Sprintf("estimated context overflow: input=%d + output reserve=%d exceeds configured limit=%d; start /new or change the limit; history was not changed", e.Tokens.InputEstimate, e.Tokens.OutputReserve, e.Tokens.ContextWindow)
}

func tokenReport(counter TokenCounter, prompt string, history, messages []Message, target Target, reserve int) TokenReport {
	r := TokenReport{LastUserMessageEstimate: counter.Count(prompt), HistoryMessages: len(history), ContextWindow: target.ContextWindow, OutputReserve: reserve}
	for _, m := range history {
		r.HistoryEstimate += counter.Count(m.Content + m.FileContext)
	}
	for _, m := range messages {
		if m.Role == "system" {
			r.SystemEstimate += counter.Count(m.Content)
		}
	}
	r.FramingEstimate = 3 + 4*len(messages)
	r.InputEstimate = r.LastUserMessageEstimate + r.HistoryEstimate + r.SystemEstimate + r.FramingEstimate
	return r
}

func (a *Agent) WithTokenCounter(counter TokenCounter) *Agent {
	copy := *a
	copy.counter = counter
	return &copy
}
