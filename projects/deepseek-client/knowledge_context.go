package main

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

// Resolve only explicit aliases and short anaphoric follow-ups. The original
// question still goes to the LLM; prior answers never become retrieval evidence.
// Explicit identifiers/codes override an inherited topic; ordinary topic
// questions are handled without a domain-specific vocabulary.
var explicitRAGSubject = regexp.MustCompile(`\b(?:[1-5][0-9][0-9]|[A-Z][A-Z0-9]+(?:[-_][A-Z0-9]+)*)\b`)

func resolveBriefTerms(query string, terms map[string]string) string {
	keys := make([]string, 0, len(terms))
	for k := range terms {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return len(keys[i]) > len(keys[j]) || (len(keys[i]) == len(keys[j]) && keys[i] < keys[j])
	})
	// Each alias is replaced once from the original text; definitions cannot
	// recursively expand other aliases or introduce regexp replacement syntax.
	if len(keys) == 0 {
		return query
	}
	quoted := make([]string, len(keys))
	values := map[string]string{}
	for i, k := range keys {
		quoted[i] = regexp.QuoteMeta(k)
		values[strings.ToLower(k)] = terms[k]
	}
	r := regexp.MustCompile(`(?i)\b(?:` + strings.Join(quoted, "|") + `)\b`)
	return r.ReplaceAllStringFunc(query, func(alias string) string { return values[strings.ToLower(alias)] })
}

func isRAGFollowup(query string) bool {
	if len(strings.Fields(query)) > 20 {
		return false
	}
	for _, word := range strings.Fields(strings.ToLower(query)) {
		word = strings.Trim(word, ".,!?;:\"'()")
		switch word {
		case "it", "its", "that", "this", "those", "его", "её", "ее", "это", "этого", "этот", "такой", "них":
			return true
		}
	}
	return false
}

func contextualKnowledgeQuery(ctx context.Context, state sessionState, prompt string) (string, error) {
	if state.Knowledge != nil {
		base, err := state.Knowledge.Selected(conversationID(state.ConversationID))
		if err != nil {
			return "", err
		}
		if base.ID == "" {
			return prompt, nil
		}
	}
	brief, err := agent.LoadTaskBrief(ctx, state.Memory, conversationID(state.ConversationID))
	if err != nil {
		return "", err
	}
	query := resolveBriefTerms(prompt, brief.Terms)
	// Explicit subjects take precedence over the previous topic.
	hasCode := query != prompt || explicitRAGSubject.MatchString(query)
	if isRAGFollowup(query) && !hasCode {
		anchor := brief.Topic
		if state.History != nil {
			messages, e := state.History.Load(ctx, conversationID(state.ConversationID))
			if e != nil {
				return "", e
			}
			for i := len(messages) - 2; i >= 0; i -= 2 {
				candidate := resolveBriefTerms(messages[i].Content, brief.Terms)
				if strings.HasPrefix(messages[i+1].Content, "Не знаю:") {
					continue
				}
				if messages[i].Role == "user" && len(candidate) <= 600 && (!isRAGFollowup(candidate) || explicitRAGSubject.MatchString(candidate)) {
					anchor = candidate
					break
				}
			}
		}
		if anchor != "" {
			query = anchor + "\nFollow-up question: " + query
		}
	}
	if len(query) > 2400 {
		return prompt, nil
	}
	return query, nil
}
