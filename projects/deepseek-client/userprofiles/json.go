package userprofiles

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

const maxBytes = 1 << 20

var validName = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,100}$`)

type JSON struct {
	Path string
	mu   sync.Mutex
}

type document struct {
	Version  int                          `json:"version"`
	Active   string                       `json:"active,omitempty"`
	Profiles map[string]agent.UserProfile `json:"profiles"`
}

func (s *JSON) load(ctx context.Context) (document, error) {
	if err := ctx.Err(); err != nil {
		return document{}, err
	}
	f, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return document{Version: 1, Profiles: map[string]agent.UserProfile{}}, nil
	}
	if err != nil {
		return document{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil || len(data) > maxBytes {
		return document{}, errors.New("user profiles file is unreadable or exceeds 1 MiB")
	}
	var doc document
	if json.Unmarshal(data, &doc) != nil || doc.Version != 1 || doc.Profiles == nil || len(doc.Profiles) > 100 {
		return document{}, errors.New("invalid user profiles JSON; file was not modified")
	}
	for name, profile := range doc.Profiles {
		if name != profile.Name || !validName.MatchString(name) || agent.ValidateUserProfile(profile) != nil {
			return document{}, errors.New("invalid user profile")
		}
	}
	if doc.Active != "" {
		if _, ok := doc.Profiles[doc.Active]; !ok {
			return document{}, errors.New("active user profile does not exist")
		}
	}
	return doc, nil
}

func (s *JSON) save(ctx context.Context, doc document) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil || len(data) > maxBytes {
		return errors.New("user profiles exceed 1 MiB")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.Path), ".profiles-*.tmp")
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
	return os.Rename(temp, s.Path)
}

func (s *JSON) Active(ctx context.Context) (agent.UserProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(ctx)
	if err != nil || doc.Active == "" {
		return agent.UserProfile{}, err
	}
	return doc.Profiles[doc.Active], nil
}

func (s *JSON) List(ctx context.Context) ([]agent.UserProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(doc.Profiles))
	for name := range doc.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	profiles := make([]agent.UserProfile, 0, len(names))
	for _, name := range names {
		profiles = append(profiles, doc.Profiles[name])
	}
	return profiles, nil
}

func (s *JSON) Get(ctx context.Context, name string) (agent.UserProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(ctx)
	if err != nil {
		return agent.UserProfile{}, err
	}
	profile, ok := doc.Profiles[name]
	if !ok {
		return agent.UserProfile{}, os.ErrNotExist
	}
	return profile, nil
}

func (s *JSON) Create(ctx context.Context, name string) (agent.UserProfile, error) {
	if !validName.MatchString(name) {
		return agent.UserProfile{}, errors.New("profile name must contain 1-100 letters, digits, dots, underscores or hyphens")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(ctx)
	if err != nil {
		return agent.UserProfile{}, err
	}
	if _, exists := doc.Profiles[name]; exists || len(doc.Profiles) == 100 {
		return agent.UserProfile{}, errors.New("profile already exists or limit reached")
	}
	profile := agent.UserProfile{Name: name}
	doc.Profiles[name], doc.Active = profile, name
	return profile, s.save(ctx, doc)
}

func (s *JSON) Save(ctx context.Context, profile agent.UserProfile) error {
	if !validName.MatchString(profile.Name) || agent.ValidateUserProfile(profile) != nil {
		return errors.New("invalid user profile")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(ctx)
	if err != nil {
		return err
	}
	if _, exists := doc.Profiles[profile.Name]; !exists {
		return os.ErrNotExist
	}
	doc.Profiles[profile.Name] = profile
	return s.save(ctx, doc)
}

func (s *JSON) Select(ctx context.Context, name string) (agent.UserProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(ctx)
	if err != nil {
		return agent.UserProfile{}, err
	}
	profile, ok := doc.Profiles[name]
	if !ok {
		return agent.UserProfile{}, os.ErrNotExist
	}
	doc.Active = name
	return profile, s.save(ctx, doc)
}

func (s *JSON) Delete(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load(ctx)
	if err != nil {
		return err
	}
	if _, ok := doc.Profiles[name]; !ok {
		return os.ErrNotExist
	}
	delete(doc.Profiles, name)
	if doc.Active == name {
		doc.Active = ""
	}
	return s.save(ctx, doc)
}
