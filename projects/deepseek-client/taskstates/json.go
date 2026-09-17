package taskstates

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

const maxBytes = 1 << 20

var validID = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,100}$`)

type JSON struct {
	Dir string
	mu  sync.Mutex
}

type document struct {
	Version        int             `json:"version"`
	ConversationID string          `json:"conversation_id"`
	State          agent.TaskState `json:"state"`
}

func (s *JSON) path(conversationID string) (string, error) {
	if !validID.MatchString(conversationID) {
		return "", errors.New("invalid conversation ID")
	}
	return filepath.Join(s.Dir, conversationID+".json"), nil
}

func (s *JSON) load(ctx context.Context, conversationID string) (document, bool, error) {
	if err := ctx.Err(); err != nil {
		return document{}, false, err
	}
	path, err := s.path(conversationID)
	if err != nil {
		return document{}, false, err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return document{Version: 1, ConversationID: conversationID}, false, nil
	}
	if err != nil {
		return document{}, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil || len(data) > maxBytes {
		return document{}, false, errors.New("task state is unreadable or exceeds 1 MiB")
	}
	var doc document
	if json.Unmarshal(data, &doc) != nil || doc.Version != 1 || doc.ConversationID != conversationID || agent.ValidateTaskState(doc.State) != nil {
		return document{}, false, errors.New("invalid task state JSON; file was not modified")
	}
	return doc, true, nil
}

func (s *JSON) save(ctx context.Context, conversationID string, state agent.TaskState) error {
	path, err := s.path(conversationID)
	if err != nil {
		return err
	}
	if err := agent.ValidateTaskState(state); err != nil {
		return err
	}
	data, err := json.MarshalIndent(document{Version: 1, ConversationID: conversationID, State: state}, "", "  ")
	if err != nil || len(data) > maxBytes {
		return errors.New("task state exceeds 1 MiB")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".task-*.tmp")
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
	return os.Rename(temp, path)
}

func (s *JSON) LoadTaskState(ctx context.Context, conversationID string) (agent.TaskState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, exists, err := s.load(ctx, conversationID)
	if err != nil || !exists {
		return agent.TaskState{}, err
	}
	return doc.State, nil
}

func (s *JSON) SaveTaskState(ctx context.Context, conversationID string, previous, next agent.TaskState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, exists, err := s.load(ctx, conversationID)
	if err != nil {
		return err
	}
	if exists {
		if doc.State != previous {
			return errors.New("task state changed concurrently")
		}
	} else if previous.Stage != "" {
		return errors.New("task state disappeared concurrently")
	}
	if err := agent.ValidateTaskTransition(previous, next); err != nil {
		return err
	}
	return s.save(ctx, conversationID, next)
}
