package history

import (
	"context"
	"errors"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

func (s *JSON) LoadFacts(ctx context.Context, id string) (agent.FactMemory, error) {
	doc, err := s.loadDocument(ctx, id)
	if err != nil {
		return agent.FactMemory{}, err
	}
	return activeData(doc).Facts, nil
}

func (s *JSON) SaveFacts(ctx context.Context, id, expected string, version int, next agent.FactMemory, usage agent.TurnUsage) error {
	doc, err := s.loadDocument(ctx, id)
	if err != nil {
		return err
	}
	data := activeData(doc)
	if agent.HistoryHash(data.Messages) != expected || data.Facts.Version != version || next.Version != version+1 {
		return errors.New("conversation or facts changed during extraction")
	}
	if next.Processed != len(data.Messages) || next.PrefixHash != expected {
		return errors.New("invalid facts progress")
	}
	data.Facts = next
	data.FactsUsage = append(data.FactsUsage, usage)
	setActiveData(&doc, data)
	return s.saveDocument(ctx, id, doc)
}

func (s *JSON) RecordFactsUsage(ctx context.Context, id string, usage agent.TurnUsage) error {
	doc, err := s.loadDocument(ctx, id)
	if err != nil {
		return err
	}
	data := activeData(doc)
	data.FactsUsage = append(data.FactsUsage, usage)
	setActiveData(&doc, data)
	return s.saveDocument(ctx, id, doc)
}
