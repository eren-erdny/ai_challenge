package agent_test

import (
	"context"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"strings"
	"testing"
)

func TestCompactGroundingKeepsProvenanceBoundary(t *testing.T) {
	for _, template := range []string{"", "compact"} {
		for _, invalid := range []bool{false, true} {
			req := requestFor(agent.Free)
			req.Target.RAGTemplate = template
			req.Evidence = evidence
			runner := agent.New(agent.ClientFunc(func(_ context.Context, _ agent.Target, _ string, s agent.Settings) (agent.Completion, error) {
				joined := ""
				for _, m := range s.Messages {
					joined += m.Content
				}
				if strings.Contains(joined, "TASK TEMPLATE:") != (template == "compact") {
					t.Fatal("template leaked across profiles")
				}
				if !s.JSONOutput || !strings.Contains(joined, "UNTRUSTED DATA") {
					t.Fatal("grounding contract weakened")
				}
				content := grounded
				if invalid {
					content = strings.Replace(content, "MUST NOT", "MUST", 1)
				}
				return agent.Completion{Content: content, FinishReason: "stop"}, nil
			}))
			_, err := runner.Run(context.Background(), req)
			if (err != nil) != invalid {
				t.Fatalf("template=%q invalid=%v error=%v", template, invalid, err)
			}
		}
	}
}
