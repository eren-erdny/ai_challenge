package support

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type User struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Plan          string `json:"plan"`
	AuthMethod    string `json:"auth_method"`
	EmailVerified *bool  `json:"email_verified"`
	AccountState  string `json:"account_state"`
}
type Ticket struct {
	ID          string `json:"id"`
	UserID      string `json:"user_id"`
	Title       string `json:"title"`
	Category    string `json:"category"`
	ErrorCode   string `json:"error_code"`
	Description string `json:"description"`
	Status      string `json:"status"`
}
type Database struct {
	Users   []User   `json:"users"`
	Tickets []Ticket `json:"tickets"`
}

var userID = regexp.MustCompile("^U-[0-9]{3}$")
var ticketID = regexp.MustCompile("^T-[0-9]{3}$")

func strict(data []byte, out any) error {
	if !utf8.Valid(data) {
		return errors.New("invalid UTF-8 JSON")
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != 123 {
		return errors.New("JSON object required")
	}
	if len(data) > 256<<10 {
		return errors.New("JSON too large")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	var parse func(int) error
	parse = func(depth int) error {
		if depth > 32 {
			return errors.New("JSON too deep")
		}
		t, e := d.Token()
		if e != nil {
			return e
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return e
					}
					s, ok := k.(string)
					if !ok || seen[s] {
						return errors.New("duplicate key")
					}
					seen[s] = true
					if e = parse(depth + 1); e != nil {
						return e
					}
				}
			case '[':
				for d.More() {
					if e = parse(depth + 1); e != nil {
						return e
					}
				}
			default:
				return errors.New("invalid delimiter")
			}
			_, e = d.Token()
			return e
		}
		return nil
	}
	if e := parse(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("trailing JSON")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(out)
}
func LoadDatabase(file string) (Database, error) {
	var db Database
	f, e := os.Open(file)
	if e != nil {
		return db, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, (256<<10)+1))
	if e != nil {
		return db, e
	}
	if e = strict(bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf}), &db); e != nil {
		return db, e
	}
	users := map[string]bool{}
	tickets := map[string]bool{}
	for _, u := range db.Users {
		if !userID.MatchString(u.ID) || users[u.ID] || u.Name == "" || u.Plan == "" || u.AuthMethod == "" || u.AccountState == "" || u.EmailVerified == nil {
			return db, errors.New("invalid user")
		}
		users[u.ID] = true
	}
	for _, t := range db.Tickets {
		if !ticketID.MatchString(t.ID) || tickets[t.ID] || !users[t.UserID] || t.Title == "" {
			return db, errors.New("invalid ticket")
		}
		tickets[t.ID] = true
	}
	return db, nil
}

type TicketRequest struct {
	TicketID string `json:"ticket_id"`
}
type TicketList struct {
	Tickets []Ticket `json:"tickets"`
}

func NewCRM(db Database, selected string) (*mcp.Server, error) {
	var current *User
	for _, u := range db.Users {
		if u.ID == selected {
			copy := u
			current = &copy
			break
		}
	}
	if current == nil {
		return nil, errors.New("unknown bound user")
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "support-json-crm", Version: "1.0.0"}, nil)
	register := func(name, description string, schema any, handler func(json.RawMessage) (any, error)) {
		server.AddTool(&mcp.Tool{Name: name, Description: description, InputSchema: schema}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			value, e := handler(req.Params.Arguments)
			if e != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "Invalid arguments or ticket not available"}}}, nil
			}
			b, e := json.Marshal(value)
			if e != nil {
				return nil, e
			}
			return &mcp.CallToolResult{StructuredContent: value, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil
		})
	}
	empty := json.RawMessage("{\"type\":\"object\",\"properties\":{},\"additionalProperties\":false}")
	noArgs := func(raw json.RawMessage) error { var a struct{}; return strict(raw, &a) }
	register("support_get_user", "Read only the operator-bound user.", empty, func(raw json.RawMessage) (any, error) {
		if e := noArgs(raw); e != nil {
			return nil, e
		}
		return *current, nil
	})
	register("support_list_tickets", "List only tickets belonging to the bound user.", empty, func(raw json.RawMessage) (any, error) {
		if e := noArgs(raw); e != nil {
			return nil, e
		}
		list := TicketList{Tickets: []Ticket{}}
		for _, t := range db.Tickets {
			if t.UserID == selected {
				list.Tickets = append(list.Tickets, t)
			}
		}
		return list, nil
	})
	schema := json.RawMessage("{\"type\":\"object\",\"properties\":{\"ticket_id\":{\"type\":\"string\",\"pattern\":\"^T-[0-9]{3}$\"}},\"required\":[\"ticket_id\"],\"additionalProperties\":false}")
	register("support_get_ticket", "Read a ticket owned by the bound user.", schema, func(raw json.RawMessage) (any, error) {
		var a TicketRequest
		if e := strict(raw, &a); e != nil || !ticketID.MatchString(a.TicketID) {
			return nil, errors.New("invalid ticket")
		}
		for _, t := range db.Tickets {
			if t.ID == a.TicketID && t.UserID == selected {
				return t, nil
			}
		}
		return nil, errors.New("ticket unavailable")
	})
	return server, nil
}
