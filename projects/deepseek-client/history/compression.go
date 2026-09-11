package history

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

func (s *JSON) LoadSummary(ctx context.Context, id string) (agent.Summary, error) {
	doc, err := s.loadDocument(ctx, id)
	data := activeData(doc)
	if err != nil || len(data.Summaries) == 0 {
		return agent.Summary{}, err
	}
	return data.Summaries[len(data.Summaries)-1], nil
}
func (s *JSON) SaveSummary(ctx context.Context, id, expected string, version int, next agent.Summary) error {
	doc, err := s.loadDocument(ctx, id)
	if err != nil {
		return err
	}
	data := activeData(doc)
	if agent.HistoryHash(data.Messages) != expected || len(data.Summaries) != version || next.Version != version+1 {
		return errors.New("conversation or summary changed during compression")
	}
	if version > 0 && next.Covered <= data.Summaries[version-1].Covered {
		return errors.New("summary boundary did not advance")
	}
	if _, err := agent.EffectiveHistory(data.Messages, next); err != nil {
		return err
	}
	data.Summaries = append(data.Summaries, next)
	setActiveData(&doc, data)
	return s.saveDocument(ctx, id, doc)
}
func (s *JSON) RecordCompressionUsage(ctx context.Context, id string, usage agent.TurnUsage) error {
	doc, err := s.loadDocument(ctx, id)
	if err != nil {
		return err
	}
	data := activeData(doc)
	data.CompressionUsage = append(data.CompressionUsage, usage)
	setActiveData(&doc, data)
	return s.saveDocument(ctx, id, doc)
}
func (s *JSON) saveDocument(ctx context.Context, id string, doc document) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > maxBytes {
		return errors.New("conversation exceeds 16 MiB; update not saved")
	}
	path, err := s.path(id)
	if err != nil {
		return err
	}
	return s.write(ctx, path, data)
}
