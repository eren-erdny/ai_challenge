package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

// JSON stores one conversation per file. Use one application instance per directory.
type JSON struct{ Dir string }

var validID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`)

type document struct {
	Version        int             `json:"version"`
	ConversationID string          `json:"conversation_id"`
	Messages       []agent.Message `json:"messages"`
}

const maxBytes = 16 << 20

func (s *JSON) path(id string) (string, error) {
	if !validID.MatchString(id) {
		return "", errors.New("invalid conversation ID")
	}
	return filepath.Join(s.Dir, id+".json"), nil
}

func validate(messages []agent.Message) error {
	if len(messages)%2 != 0 {
		return errors.New("incomplete conversation turn")
	}
	for i, message := range messages {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if message.Role != role || message.Content == "" {
			return errors.New("invalid conversation message")
		}
	}
	return nil
}

func (s *JSON) Load(ctx context.Context, id string) ([]agent.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.path(id)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, errors.New("conversation exceeds 16 MiB; archive it before continuing")
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, errors.New("invalid conversation JSON; file was not modified")
	}
	if doc.Version != 1 || doc.ConversationID != id {
		return nil, errors.New("unsupported conversation version or ID")
	}
	if err := validate(doc.Messages); err != nil {
		return nil, err
	}
	return doc.Messages, ctx.Err()
}

func (s *JSON) Save(ctx context.Context, id string, messages []agent.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.path(id)
	if err != nil {
		return err
	}
	if err := validate(messages); err != nil {
		return err
	}
	data, err := json.MarshalIndent(document{1, id, messages}, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > maxBytes {
		return errors.New("conversation exceeds 16 MiB; history was not saved")
	}
	return s.write(ctx, path, data)
}

func (s *JSON) write(ctx context.Context, path string, data []byte) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Dir, ".conversation-*.tmp")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// Never delete the old history if replacement fails.
	if err = os.Rename(temp, path); err != nil {
		return fmt.Errorf("replace conversation: %w", err)
	}
	return nil
}
