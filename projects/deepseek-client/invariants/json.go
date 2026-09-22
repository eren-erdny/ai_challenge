package invariants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

const maxBytes = 1 << 20

type JSON struct {
	Path string
	mu   sync.Mutex
}

type document struct {
	Version    int               `json:"version"`
	NextID     int               `json:"next_id"`
	Invariants []agent.Invariant `json:"invariants"`
}

func (s *JSON) load(ctx context.Context) (document, error) {
	if err := ctx.Err(); err != nil {
		return document{}, err
	}
	f, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return document{Version: 1, NextID: 1, Invariants: []agent.Invariant{}}, nil
	}
	if err != nil {
		return document{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil || len(data) > maxBytes {
		return document{}, errors.New("invariants file is unreadable or exceeds 1 MiB")
	}
	var doc document
	if json.Unmarshal(data, &doc) != nil || doc.Version != 1 || agent.ValidateInvariants(doc.Invariants) != nil {
		return document{}, errors.New("invalid invariants JSON; file was not modified")
	}
	slices.SortFunc(doc.Invariants, func(a, b agent.Invariant) int { return a.ID - b.ID })
	minimumNext := 1
	if len(doc.Invariants) > 0 {
		minimumNext = doc.Invariants[len(doc.Invariants)-1].ID + 1
	}
	if doc.NextID == 0 {
		doc.NextID = minimumNext
	}
	if doc.NextID < minimumNext {
		return document{}, errors.New("invalid invariant next ID")
	}
	return doc, nil
}

func (s *JSON) LoadInvariants(ctx context.Context) ([]agent.Invariant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(ctx)
	return append([]agent.Invariant(nil), doc.Invariants...), err
}

func (s *JSON) AddInvariant(ctx context.Context, kind agent.InvariantKind, text string) (agent.Invariant, error) {
	text = strings.TrimSpace(text)
	if !agent.ValidInvariantKind(kind) || text == "" || len(text) > 2000 {
		return agent.Invariant{}, errors.New("invariant requires a valid kind and 1-2000 bytes of text")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(ctx)
	if err != nil {
		return agent.Invariant{}, err
	}
	if len(doc.Invariants) == 100 {
		return agent.Invariant{}, errors.New("invariant store already contains 100 entries")
	}
	id := doc.NextID
	doc.NextID++
	value := agent.Invariant{ID: id, Kind: kind, Text: text, Updated: time.Now().UTC()}
	doc.Invariants = append(doc.Invariants, value)
	return value, s.save(ctx, doc)
}

func (s *JSON) DeleteInvariant(ctx context.Context, id int) (bool, error) {
	if id < 1 {
		return false, errors.New("invalid invariant ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(ctx)
	if err != nil {
		return false, err
	}
	for index, value := range doc.Invariants {
		if value.ID == id {
			doc.Invariants = append(doc.Invariants[:index], doc.Invariants[index+1:]...)
			return true, s.save(ctx, doc)
		}
	}
	return false, nil
}

func (s *JSON) save(ctx context.Context, doc document) error {
	if err := agent.ValidateInvariants(doc.Invariants); err != nil {
		return err
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil || len(data) > maxBytes {
		return errors.New("invariants exceed 1 MiB")
	}
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".invariants-*.tmp")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(temp, s.Path); err != nil {
		return fmt.Errorf("replace invariants: %w", err)
	}
	return nil
}
