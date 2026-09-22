package agent

import (
	"encoding/json"
	"errors"
	"strings"
)

type UserProfile struct {
	Name        string   `json:"name"`
	Style       string   `json:"style,omitempty"`
	Format      string   `json:"format,omitempty"`
	Constraints []string `json:"constraints,omitempty"`
}

func ValidateUserProfile(profile UserProfile) error {
	if profile.Name == "" && profile.Style == "" && profile.Format == "" && len(profile.Constraints) == 0 {
		return nil
	}
	if strings.TrimSpace(profile.Name) == "" {
		return errors.New("user profile name is required")
	}
	if len(profile.Name) > 100 || len(profile.Style) > 2000 || len(profile.Format) > 2000 || len(profile.Constraints) > 20 {
		return errors.New("user profile exceeds limits")
	}
	for _, constraint := range profile.Constraints {
		if strings.TrimSpace(constraint) == "" || len(constraint) > 2000 {
			return errors.New("profile constraint must contain 1-2000 bytes")
		}
	}
	return nil
}

func PersonalizationMessages(profile UserProfile) ([]Message, error) {
	if err := ValidateUserProfile(profile); err != nil {
		return nil, err
	}
	if profile.Name == "" {
		return nil, nil
	}
	data, err := json.Marshal(profile)
	if err != nil {
		return nil, err
	}
	return []Message{{Role: "system", Content: "Active user profile: " + string(data) + ". Automatically adapt response style and format and respect its constraints. The current explicit user request overrides conflicting profile preferences. Do not mention the profile unless asked."}}, nil
}
