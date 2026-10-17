package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type guardExecutor struct{ calls []string }

func (*guardExecutor) Definitions() []ToolDefinition {
	return []ToolDefinition{
		{Type: "function", Function: ToolFunction{Name: "read_file", Parameters: []byte(`{"type":"object","properties":{"path":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":20}},"required":["path"],"additionalProperties":false}`)}},
		{Type: "function", Function: ToolFunction{Name: "write_file", Parameters: []byte(`{"type":"object"}`)}},
		{Type: "function", Function: ToolFunction{Name: "git__git_status", Parameters: []byte(`{"type":"object","additionalProperties":false}`)}},
		{Type: "function", Function: ToolFunction{Name: "other__git_status", Parameters: []byte(`{"type":"object"}`)}},
	}
}
func (e *guardExecutor) Execute(_ context.Context, name, _ string) (string, error) {
	e.calls = append(e.calls, name)
	return "ok", nil
}
func guardCall(id, name, args string) ToolCall {
	return ToolCall{ID: id, Type: "function", Function: FunctionCall{Name: name, Arguments: args}}
}
func TestGuardRejectsInvalidCallsBeforeExecution(t *testing.T) {
	for _, tc := range []struct{ name, args string }{
		{"missing", "{}"}, {"read_file", "null"},
		{"read_file", `{"path":"../secret.md"}`},
		{"read_file", `{"path":"C:/secret.md"}`},
		{"read_file", `{"path":".git/config"}`}, {"read_file", "[]"},
		{"read_file", "{}"}, {"read_file", `{"path":2}`},
		{"read_file", `{"path":"a","limit":21}`},
		{"read_file", `{"path":"a","limit":1.5}`},
		{"read_file", `{"path":"a","extra":true}`},
		{"read_file", `{"path":"a","path":"b"}`},
		{"read_file", `{"path":"a"} {}`},
		{"read_file", strings.Repeat(" ", 2<<20+1)},
	} {
		t.Run(tc.name+tc.args[:min(len(tc.args), 32)], func(t *testing.T) {
			e := &guardExecutor{}
			rounds := 0
			client := ClientFunc(func(_ context.Context, _ Target, _ string, s Settings) (Completion, error) {
				rounds++
				if rounds == 1 {
					return Completion{ToolCalls: []ToolCall{guardCall("bad", tc.name, tc.args)}}, nil
				}
				last := s.Messages[len(s.Messages)-1]
				if !strings.Contains(last.Content, `"executed":false`) {
					t.Fatalf("missing structured denial: %s", last.Content)
				}
				return Completion{Content: "denied"}, nil
			})
			_, err := New(client).WithTools(e).Run(context.Background(), Request{Prompt: "work", Mode: Free, Target: Target{Model: "test"}})
			if err != nil || len(e.calls) != 0 {
				t.Fatalf("err=%v effects=%v", err, e.calls)
			}
		})
	}
}
func TestReadOnlyGuardMixedBatchAndMCPNames(t *testing.T) {
	e := &guardExecutor{}
	rounds := 0
	client := ClientFunc(func(_ context.Context, _ Target, _ string, s Settings) (Completion, error) {
		rounds++
		if rounds == 1 {
			for _, def := range s.Tools {
				if def.Function.Name == "write_file" || def.Function.Name == "other__git_status" {
					t.Fatal("unsafe tool advertised")
				}
			}
			return Completion{ToolCalls: []ToolCall{
				guardCall("read", "read_file", `{"path":"README.md"}`),
				guardCall("write", "write_file", "{}"),
				guardCall("mcp", "git__git_status", "{}"),
				guardCall("other", "other__git_status", "{}"),
			}}, nil
		}
		return Completion{Content: "done"}, nil
	})
	_, err := New(client).WithTools(e).Run(context.Background(), Request{Prompt: "explain", Mode: Free, ReadOnlyTools: true, Target: Target{Model: "test"}})
	if err != nil || strings.Join(e.calls, ",") != "read_file,git__git_status" {
		t.Fatalf("err=%v effects=%v", err, e.calls)
	}
}

type guardInvariants struct{}

func (guardInvariants) LoadInvariants(context.Context) ([]Invariant, error) {
	return []Invariant{{ID: 1, Kind: InvariantBusiness, Text: "Do not write files", Updated: time.Now()}}, nil
}
func TestToolInvariantGateAndUsage(t *testing.T) {
	for _, tc := range []struct {
		name, verdict string
		checkerError  bool
		wantEffects   int
	}{
		{"allow", `{"compliant":true,"violations":[],"explanation":""}`, false, 1},
		{"deny", `{"compliant":false,"violations":[1],"explanation":"writing forbidden"}`, false, 0},
		{"malformed", "oops", false, 0},
		{"missing", `{"compliant":true}`, false, 0},
		{"duplicate", `{"compliant":true,"compliant":true,"violations":[],"explanation":""}`, false, 0},
		{"timeout", "", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := &guardExecutor{}
			actorCalls, checks := 0, 0
			client := ClientFunc(func(_ context.Context, _ Target, prompt string, s Settings) (Completion, error) {
				for _, m := range s.Messages {
					if m.Role == "system" && strings.Contains(m.Content, "fail-closed invariant compliance checker") {
						if len(s.Tools) != 0 {
							t.Fatal("checker has tools")
						}
						if strings.Contains(m.Content, "pre-execution tool safety check") {
							checks++
							if len(e.calls) != 0 {
								t.Fatal("executed before checker")
							}
							if !strings.Contains(prompt, "tool_call") || !strings.Contains(prompt, "write_file") {
								t.Fatal("missing action context")
							}
							if tc.checkerError {
								return Completion{}, context.DeadlineExceeded
							}
							return Completion{Content: tc.verdict, PromptTokens: 5, CompletionTokens: 1, UsageKnown: true}, nil
						}
						return Completion{Content: `{"compliant":true,"violations":[],"explanation":""}`, PromptTokens: 5, CompletionTokens: 1, UsageKnown: true}, nil
					}
				}
				actorCalls++
				if actorCalls == 1 {
					return Completion{ToolCalls: []ToolCall{guardCall("call", "write_file", `{"path":"out.md"}`)}, PromptTokens: 1, CompletionTokens: 1, UsageKnown: true}, nil
				}
				return Completion{Content: "done", PromptTokens: 1, CompletionTokens: 1, UsageKnown: true}, nil
			})
			result, err := New(client).WithTools(e).WithInvariants(guardInvariants{}).Run(context.Background(), Request{Prompt: "write file", Mode: Free, Target: Target{Model: "test"}})
			if checks != 1 || len(e.calls) != tc.wantEffects {
				t.Fatalf("checks=%d effects=%v err=%v", checks, e.calls, err)
			}
			if tc.name == "allow" || tc.name == "deny" {
				if err != nil || result.Last().Tokens.TotalActual != 16 {
					t.Fatalf("usage/result=%+v err=%v", result, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "failed closed") {
				t.Fatalf("expected closed failure: %v", err)
			} else if !tc.checkerError && (result.Failed == nil || result.Failed.Tokens.TotalActual != 8) {
				t.Fatalf("failed usage lost: %+v", result.Failed)
			}
		})
	}
}
func TestInvalidToolDefinitionsFailClosed(t *testing.T) {
	defs := (&guardExecutor{}).Definitions()
	for _, bad := range [][]ToolDefinition{
		append(defs, defs[0]),
		{{Type: "function", Function: ToolFunction{Name: "bad", Parameters: []byte(`{"$ref":"https://example.invalid/schema"}`)}}},
		{{Type: "function", Function: ToolFunction{Name: "bad", Parameters: []byte("null")}}},
	} {
		_, err := newToolGuard(bad)
		var denied *ToolGuardError
		if !errors.As(err, &denied) {
			t.Fatalf("expected invalid definition rejection: %v", err)
		}
	}
}
