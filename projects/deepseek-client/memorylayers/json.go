package memorylayers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

const maxBytes = 1 << 20

var validPart = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,100}$`)

type JSON struct {
	Dir string
	mu  sync.Mutex
}

type document struct {
	Version        int                          `json:"version"`
	Layer          agent.MemoryLayer            `json:"layer"`
	ConversationID string                       `json:"conversation_id,omitempty"`
	Entries        map[string]agent.MemoryEntry `json:"entries"`
}

func validateTarget(conversationID string, layer agent.MemoryLayer) error {
	if !agent.ValidMemoryLayer(layer) {
		return errors.New("invalid memory layer")
	}
	if layer == agent.MemoryWorking && !validPart.MatchString(conversationID) {
		return errors.New("invalid conversation ID")
	}
	return nil
}

func validateEntry(key, value string) error {
	if !validPart.MatchString(key) {
		return errors.New("memory key must contain 1-100 letters, digits, dots, underscores or hyphens")
	}
	if strings.TrimSpace(value) == "" || len(value) > 4000 {
		return errors.New("memory value must contain 1-4000 bytes")
	}
	return nil
}

func (s *JSON) path(conversationID string, layer agent.MemoryLayer) (string, error) {
	if err := validateTarget(conversationID, layer); err != nil {
		return "", err
	}
	if layer == agent.MemoryLongTerm {
		return filepath.Join(s.Dir, "long-term.json"), nil
	}
	return filepath.Join(s.Dir, "working", conversationID+".json"), nil
}

func (s *JSON) load(ctx context.Context, conversationID string, layer agent.MemoryLayer) (document, error) {
	if err := ctx.Err(); err != nil {
		return document{}, err
	}
	path, err := s.path(conversationID, layer)
	if err != nil {
		return document{}, err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		documentConversationID := conversationID
		if layer == agent.MemoryLongTerm {
			documentConversationID = ""
		}
		return document{Version: 1, Layer: layer, ConversationID: documentConversationID, Entries: map[string]agent.MemoryEntry{}}, nil
	}
	if err != nil {
		return document{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return document{}, err
	}
	if len(data) > maxBytes {
		return document{}, errors.New("memory layer exceeds 1 MiB")
	}
	var doc document
	if json.Unmarshal(data, &doc) != nil || doc.Version != 1 || doc.Layer != layer {
		return document{}, errors.New("invalid memory layer JSON; file was not modified")
	}
	if layer == agent.MemoryWorking && doc.ConversationID != conversationID {
		return document{}, errors.New("memory layer conversation ID mismatch")
	}
	if doc.Entries == nil || len(doc.Entries) > 100 {
		return document{}, errors.New("invalid memory entries")
	}
	for key, entry := range doc.Entries {
		if err := validateEntry(key, entry.Value); err != nil || entry.Updated.IsZero() {
			return document{}, errors.New("invalid memory entry")
		}
	}
	return doc, nil
}

func cloneEntries(entries map[string]agent.MemoryEntry) map[string]agent.MemoryEntry {
	copy := make(map[string]agent.MemoryEntry, len(entries))
	for key, entry := range entries {
		copy[key] = entry
	}
	return copy
}

func (s *JSON) LoadMemory(ctx context.Context, conversationID string, layer agent.MemoryLayer) (map[string]agent.MemoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(ctx, conversationID, layer)
	if err != nil {
		return nil, err
	}
	return cloneEntries(doc.Entries), nil
}

func (s *JSON) SetMemory(ctx context.Context, conversationID string, layer agent.MemoryLayer, key, value string) error {
	if err := validateEntry(key, value); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(ctx, conversationID, layer)
	if err != nil {
		return err
	}
	if _, exists := doc.Entries[key]; !exists && len(doc.Entries) == 100 {
		return errors.New("memory layer already contains 100 keys")
	}
	doc.Entries[key] = agent.MemoryEntry{Value: value, Updated: time.Now().UTC()}
	return s.save(ctx, conversationID, layer, doc)
}

func (s *JSON) DeleteMemory(ctx context.Context, conversationID string, layer agent.MemoryLayer, key string) (bool, error) {
	if !validPart.MatchString(key) {
		return false, errors.New("invalid memory key")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(ctx, conversationID, layer)
	if err != nil {
		return false, err
	}
	if _, exists := doc.Entries[key]; !exists {
		return false, nil
	}
	delete(doc.Entries, key)
	return true, s.save(ctx, conversationID, layer, doc)
}

func (s *JSON) save(ctx context.Context, conversationID string, layer agent.MemoryLayer, doc document) error {
	path, err := s.path(conversationID, layer)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > maxBytes {
		return errors.New("memory layer exceeds 1 MiB; update not saved")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".memory-*.tmp")
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
	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("replace memory layer: %w", err)
	}
	return nil
}
