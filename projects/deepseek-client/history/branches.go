package history

import (
	"context"
	"errors"
	"sort"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

func validBranchPart(name string) bool {
	return validID.MatchString(name)
}

func (s *JSON) CreateCheckpoint(ctx context.Context, id, name string) error {
	if !validBranchPart(name) {
		return errors.New("invalid checkpoint name")
	}
	doc, err := s.loadDocument(ctx, id)
	if err != nil {
		return err
	}
	if _, exists := doc.Checkpoints[name]; exists {
		return errors.New("checkpoint already exists")
	}
	if doc.Checkpoints == nil {
		doc.Checkpoints = map[string]checkpoint{}
	}
	data := activeData(doc)
	messages := append([]agent.Message(nil), data.Messages...)
	usage := append([]agent.TurnUsage(nil), data.Usage...)
	doc.Checkpoints[name] = checkpoint{SourceBranch: activeBranch(doc), Messages: messages, Usage: usage}
	return s.saveDocument(ctx, id, doc)
}

func (s *JSON) CreateBranch(ctx context.Context, id, name, checkpointName string) error {
	if !validBranchPart(name) || name == mainBranch {
		return errors.New("invalid or reserved branch name")
	}
	if !validBranchPart(checkpointName) {
		return errors.New("invalid checkpoint name")
	}
	doc, err := s.loadDocument(ctx, id)
	if err != nil {
		return err
	}
	if _, exists := doc.Branches[name]; exists {
		return errors.New("branch already exists")
	}
	checkpoint, exists := doc.Checkpoints[checkpointName]
	if !exists {
		return errors.New("checkpoint does not exist")
	}
	if doc.Branches == nil {
		doc.Branches = map[string]branchData{}
	}
	doc.Branches[name] = branchData{Messages: append([]agent.Message(nil), checkpoint.Messages...), Usage: append([]agent.TurnUsage(nil), checkpoint.Usage...)}
	return s.saveDocument(ctx, id, doc)
}

func (s *JSON) SwitchBranch(ctx context.Context, id, name string) ([]agent.Message, error) {
	if !validBranchPart(name) {
		return nil, errors.New("invalid branch name")
	}
	doc, err := s.loadDocument(ctx, id)
	if err != nil {
		return nil, err
	}
	if name != mainBranch {
		if _, exists := doc.Branches[name]; !exists {
			return nil, errors.New("branch does not exist")
		}
	}
	doc.ActiveBranch = name
	if err := s.saveDocument(ctx, id, doc); err != nil {
		return nil, err
	}
	return append([]agent.Message(nil), activeData(doc).Messages...), nil
}

func (s *JSON) Branches(ctx context.Context, id string) ([]agent.BranchInfo, []string, error) {
	doc, err := s.loadDocument(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	active := activeBranch(doc)
	branches := []agent.BranchInfo{{Name: mainBranch, Active: active == mainBranch, Messages: len(doc.Messages)}}
	for name, data := range doc.Branches {
		branches = append(branches, agent.BranchInfo{Name: name, Active: active == name, Messages: len(data.Messages)})
	}
	sort.Slice(branches, func(i, j int) bool { return branches[i].Name < branches[j].Name })
	checkpoints := make([]string, 0, len(doc.Checkpoints))
	for name := range doc.Checkpoints {
		checkpoints = append(checkpoints, name)
	}
	sort.Strings(checkpoints)
	return branches, checkpoints, nil
}
