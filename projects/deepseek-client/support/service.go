package support

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

type CRM interface {
	Execute(context.Context, string, string) (string, error)
}
type AskRequest struct {
	Question string `json:"question"`
	TicketID string `json:"ticket_id"`
}
type Trace struct {
	Tool   string `json:"tool"`
	Source string `json:"source"`
}
type Reply struct {
	Answer    *agent.GroundedAnswer `json:"answer"`
	User      User                  `json:"user"`
	Ticket    *Ticket               `json:"ticket,omitempty"`
	Retrieval []Chunk               `json:"retrieval"`
	MCP       []Trace               `json:"mcp"`
	Usage     Usage                 `json:"usage"`
}
type Service struct {
	CRM       CRM
	Corpus    Corpus
	Client    agent.Client
	Budget    *Budget
	BoundUser string
}

func (s *Service) Tickets(ctx context.Context) (TicketList, error) {
	text, e := s.CRM.Execute(ctx, "support_list_tickets", "{}")
	if e != nil {
		return TicketList{}, e
	}
	var list TicketList
	if e = strict([]byte(text), &list); e != nil {
		return list, e
	}
	for _, t := range list.Tickets {
		if t.UserID != s.BoundUser {
			return TicketList{}, errors.New("MCP scope mismatch")
		}
	}
	return list, nil
}
func (s *Service) Ask(ctx context.Context, req AskRequest) (Reply, error) {
	result := Reply{Retrieval: []Chunk{}, MCP: []Trace{}}
	if strings.TrimSpace(req.Question) == "" || utf8.RuneCountInString(req.Question) > 2000 || !utf8.ValidString(req.Question) {
		return result, errors.New("invalid question")
	}
	if req.TicketID != "" && !ticketID.MatchString(req.TicketID) {
		return result, errors.New("invalid ticket ID")
	}
	user, e := s.CRM.Execute(ctx, "support_get_user", "{}")
	if e != nil {
		return result, e
	}
	if strict([]byte(user), &result.User) != nil || result.User.ID != s.BoundUser {
		return result, errors.New("MCP user mismatch")
	}
	contextChunks := []Chunk{{ID: "crm-user", Source: "mcp://support/users/" + s.BoundUser, Section: "Текущий пользователь", Text: user}}
	result.MCP = append(result.MCP, Trace{"support_get_user", contextChunks[0].Source})
	query := req.Question
	if req.TicketID != "" {
		args, _ := json.Marshal(TicketRequest{TicketID: req.TicketID})
		text, e := s.CRM.Execute(ctx, "support_get_ticket", string(args))
		if e != nil {
			return result, e
		}
		var ticket Ticket
		if strict([]byte(text), &ticket) != nil || ticket.ID != req.TicketID || ticket.UserID != s.BoundUser {
			return result, errors.New("MCP ticket mismatch")
		}
		result.Ticket = &ticket
		query += " " + ticket.Category + " " + ticket.ErrorCode
		contextChunks = append(contextChunks, Chunk{ID: "crm-ticket", Source: "mcp://support/tickets/" + ticket.ID, Section: "Текущий тикет", Text: text})
		result.MCP = append(result.MCP, Trace{"support_get_ticket", contextChunks[1].Source})
	}
	result.Retrieval = s.Corpus.Search(query)
	evidence, _ := json.Marshal(struct {
		Results []Chunk `json:"results"`
		Context []Chunk `json:"context_results"`
	}{result.Retrieval, contextChunks})
	if len(result.Retrieval) == 0 {
		result.Answer = &agent.GroundedAnswer{Status: "unknown", Answer: "В FAQ нет подтверждений для ответа.", Clarification: "Уточните вопрос или передайте обращение специалисту поддержки.", Claims: []agent.GroundedClaim{}, Quotes: []agent.GroundedQuote{}, Sources: []agent.GroundedSource{}, Local: true}
		result.Usage = s.Budget.Snapshot()
		return result, nil
	}
	request := agent.Request{Prompt: req.Question, Mode: agent.Free, Evidence: string(evidence), Temperature: 0, Target: agent.Target{Profile: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-flash", SendThinkingDisabled: true, MaxOutputTokens: 1200, ContextWindow: 65536}}
	response, e := agent.New(s.Budget.Client(s.Client)).Run(ctx, request)
	result.Usage = s.Budget.Snapshot()
	if e != nil {
		return result, errors.New("support answer rejected or provider unavailable")
	}
	last := response.Last()
	if last == nil || last.Grounding == nil {
		return result, errors.New("missing grounded support answer")
	}
	result.Answer = last.Grounding
	if result.Answer.Status == "answered" {
		docs, mcp := false, false
		for _, source := range result.Answer.Sources {
			docs = docs || strings.HasPrefix(source.Source, "faq/")
			mcp = mcp || strings.HasPrefix(source.Source, "mcp://support/")
		}
		if !docs || (req.TicketID != "" && !mcp) {
			return result, errors.New("personalized answer must cite FAQ and MCP context")
		}
	}
	return result, nil
}
