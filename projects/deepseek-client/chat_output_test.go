package main

import (
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

func TestChatOutputHidesMetricsButKeepsWarnings(t *testing.T) {
	cost := 0.25
	r := agent.Response{Answer: agent.Completion{Content: "Answer text", PromptTokens: 100, CompletionTokens: 10, UsageKnown: true}, Tokens: agent.TokenReport{InputActual: 100, OutputActual: 10, UsageKnown: true}, CostUSD: &cost}
	var output strings.Builder
	printAgentResponse(&output, r)
	if output.String() != "Answer text\n" {
		t.Fatalf("unexpected chat metadata: %q", output.String())
	}
	r.Answer.FinishReason = "length"
	output.Reset()
	printAgentResponse(&output, r)
	if !strings.Contains(output.String(), "ответ обрезан") {
		t.Fatal("truncation warning lost")
	}
	output.Reset()
	printRequestStatus(&output, &requestStatus{Result: r.Answer, Tokens: &r.Tokens})
	if !strings.Contains(output.String(), "вход=100") || !strings.Contains(output.String(), "ответ=10") {
		t.Fatal("status lost metrics")
	}
}
