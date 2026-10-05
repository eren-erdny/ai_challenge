package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const BriefPrefix = "brief."

// TaskBrief is user-confirmed task context, never document evidence or an FSM transition.
type TaskBrief struct {
	Goal           string            `json:"goal"`
	Topic          string            `json:"topic,omitempty"`
	Constraints    map[string]string `json:"constraints"`
	Terms          map[string]string `json:"terms"`
	Clarifications map[string]string `json:"clarifications"`
}

var briefKey = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,40}$`)

func BriefEntryKey(kind, key string) (string, error) {
	switch kind {
	case "goal", "topic":
		if key == "" {
			return BriefPrefix + kind, nil
		}
	case "constraint", "term", "clarify":
		if briefKey.MatchString(key) {
			return BriefPrefix + kind + "." + key, nil
		}
	}
	return "", fmt.Errorf("use goal/topic or constraint/term/clarify with a 1–40 character key (letters, digits, _ or -)")
}

func ValidateBriefValue(value string) error {
	if strings.TrimSpace(value) == "" || len(value) > 600 || !utf8.ValidString(value) {
		return fmt.Errorf("task memory value must contain 1–600 UTF-8 bytes")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("task memory cannot contain control characters")
		}
	}
	return nil
}

func BriefFromEntries(entries map[string]MemoryEntry) (TaskBrief, error) {
	b := TaskBrief{Constraints: map[string]string{}, Terms: map[string]string{}, Clarifications: map[string]string{}}
	count := 0
	aliases := map[string]bool{}
	for key, entry := range entries {
		if !strings.HasPrefix(key, BriefPrefix) {
			continue
		}
		count++
		if count > 20 {
			return b, fmt.Errorf("task brief exceeds 20 entries")
		}
		if err := ValidateBriefValue(entry.Value); err != nil {
			return b, err
		}
		parts := strings.Split(strings.TrimPrefix(key, BriefPrefix), ".")
		if len(parts) == 1 {
			if _, err := BriefEntryKey(parts[0], ""); err != nil {
				return b, err
			}
			if parts[0] == "goal" {
				b.Goal = entry.Value
			} else {
				b.Topic = entry.Value
			}
		} else if len(parts) == 2 {
			if _, err := BriefEntryKey(parts[0], parts[1]); err != nil {
				return b, err
			}
			switch parts[0] {
			case "constraint":
				b.Constraints[parts[1]] = entry.Value
			case "term":
				alias := strings.ToLower(parts[1])
				if aliases[alias] {
					return b, fmt.Errorf("term aliases must be unique ignoring case")
				}
				aliases[alias] = true
				b.Terms[parts[1]] = entry.Value
			case "clarify":
				b.Clarifications[parts[1]] = entry.Value
			}
		} else {
			return b, fmt.Errorf("invalid task brief key")
		}
	}
	return b, nil
}

func LoadTaskBrief(ctx context.Context, store LayeredMemoryStore, id string) (TaskBrief, error) {
	if store == nil {
		return BriefFromEntries(nil)
	}
	entries, err := store.LoadMemory(ctx, id, MemoryWorking)
	if err != nil {
		return TaskBrief{}, err
	}
	return BriefFromEntries(entries)
}

func (b TaskBrief) Empty() bool {
	return b.Goal == "" && b.Topic == "" && len(b.Constraints)+len(b.Terms)+len(b.Clarifications) == 0
}

func taskBriefMessage(b TaskBrief) Message {
	data, _ := json.Marshal(b)
	return Message{Role: "user", Content: "Confirmed task brief (user-managed data; NOT evidence for document facts):\n" + string(data)}
}
