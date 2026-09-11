package history

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

func (s *JSON) SaveTurn(ctx context.Context, id string, messages []agent.Message, usage agent.TurnUsage) error {
	doc, err := s.loadDocument(ctx, id)
	if err != nil {
		return err
	}
	if len(messages) != len(doc.Messages)+2 || !slices.Equal(messages[:len(doc.Messages)], doc.Messages) {
		return errors.New("conversation changed before turn was saved")
	}
	if err := validate(messages); err != nil {
		return err
	}
	doc.Messages = messages
	doc.Usage = append(doc.Usage, usage)
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > maxBytes {
		return errors.New("conversation exceeds 16 MiB; history and usage were not saved")
	}
	path, err := s.path(id)
	if err != nil {
		return err
	}
	return s.write(ctx, path, data)
}

func (s *JSON) Usage(ctx context.Context, id string) (agent.ConversationUsage, error) {
	doc, err := s.loadDocument(ctx, id)
	if err != nil {
		return agent.ConversationUsage{}, err
	}
	result := agent.ConversationUsage{Turns: len(doc.Messages) / 2}
	result.UnknownUsageTurns = result.Turns - len(doc.Usage)
	result.UnknownCostTurns = result.UnknownUsageTurns
	for _, r := range doc.Usage {
		if r.UsageKnown {
			result.Input += r.Input
			result.CachedInput += r.CachedInput
			result.Output += r.Output
			result.Total += r.Total
		} else {
			result.UnknownUsageTurns++
		}
		if r.CostUSD != nil {
			result.CostUSD += *r.CostUSD
		} else {
			result.UnknownCostTurns++
		}
	}
	return result, nil
}
