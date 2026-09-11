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
	branch := activeData(doc)
	if len(messages) != len(branch.Messages)+2 || !slices.Equal(messages[:len(branch.Messages)], branch.Messages) {
		return errors.New("conversation changed before turn was saved")
	}
	if err := validate(messages); err != nil {
		return err
	}
	branch.Messages = messages
	branch.Usage = append(branch.Usage, usage)
	setActiveData(&doc, branch)
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
	data := activeData(doc)
	result := agent.ConversationUsage{Turns: len(data.Messages) / 2}
	result.UnknownUsageTurns = result.Turns - len(data.Usage)
	result.UnknownCostTurns = result.UnknownUsageTurns
	result.CompressionCalls = len(data.CompressionUsage)
	if len(data.Summaries) > 0 {
		current := data.Summaries[len(data.Summaries)-1]
		result.SummaryVersion = current.Version
		result.SummaryCoveredMessages = current.Covered
	}
	for _, r := range data.CompressionUsage {
		if r.UsageKnown {
			result.CompressionInput += r.Input
			result.CompressionOutput += r.Output
			result.CompressionTotal += r.Total
		}
		if r.CostUSD != nil {
			result.CompressionCostUSD += *r.CostUSD
		}
	}
	records := append(append(append([]agent.TurnUsage(nil), data.Usage...), data.CompressionUsage...), data.FactsUsage...)
	for _, r := range records {
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
	result.FactsCalls = len(data.FactsUsage)
	result.FactVersion = data.Facts.Version
	result.FactKeys = len(data.Facts.Values)
	for _, r := range data.FactsUsage {
		if r.UsageKnown {
			result.FactsInput += r.Input
			result.FactsOutput += r.Output
			result.FactsTotal += r.Total
		}
		if r.CostUSD != nil {
			result.FactsCostUSD += *r.CostUSD
		}
	}
	return result, nil
}
