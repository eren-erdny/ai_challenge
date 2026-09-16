package history

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

type activeConversation struct {
	ID string `json:"conversation_id"`
}

// Active restores selection and migrates the original default conversation.
func (s *JSON) Active(ctx context.Context) (string, []agent.Message, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, ".active.json"))
	if errors.Is(err, os.ErrNotExist) {
		path, _ := s.path("default")
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			if err := s.Save(ctx, "default", []agent.Message{}); err != nil {
				return "", nil, err
			}
		} else if err != nil {
			return "", nil, err
		}
		messages, err := s.Select(ctx, "default")
		return "default", messages, err
	}
	if err != nil {
		return "", nil, err
	}
	var active activeConversation
	if err := json.Unmarshal(data, &active); err != nil {
		return "", nil, errors.New("invalid active conversation file")
	}
	messages, err := s.loadExisting(ctx, active.ID)
	return active.ID, messages, err
}

func (s *JSON) loadExisting(ctx context.Context, id string) ([]agent.Message, error) {
	path, err := s.path(id)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return s.Load(ctx, id)
}

// Select never creates a conversation for an unknown ID.
func (s *JSON) Select(ctx context.Context, id string) ([]agent.Message, error) {
	messages, err := s.loadExisting(ctx, id)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(activeConversation{ID: id})
	if err != nil {
		return nil, err
	}
	if err := s.write(ctx, filepath.Join(s.Dir, ".active.json"), data); err != nil {
		return nil, err
	}
	return messages, nil
}

func (s *JSON) NewConversation(ctx context.Context) (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(random[:])
	path, _ := s.path(id)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return "", err
		}
		return "", errors.New("conversation ID already exists; retry")
	}
	if err := s.Save(ctx, id, []agent.Message{}); err != nil {
		return "", err
	}
	if _, err := s.Select(ctx, id); err != nil {
		return "", err
	}
	return id, nil
}
