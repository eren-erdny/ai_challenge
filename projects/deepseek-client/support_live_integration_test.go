package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/support"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSupportAssistantLive(t *testing.T) {
	if os.Getenv("SUPPORT_LIVE") != "1" {
		t.Skip("explicitly authorized paid verification only")
	}
	if os.Getenv("SUPPORT_BUDGET_USD") != "0.20" {
		t.Fatal("current 0.20 USD authorization required")
	}
	output := os.Getenv("SUPPORT_VERIFY_DIR")
	if output == "" {
		t.Fatal("isolated verification directory required")
	}
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal("fresh verification directory required")
	}
	configPath, e := defaultConfigPath()
	if e != nil {
		t.Fatal("configuration unavailable")
	}
	config, e := loadConfig(configPath)
	if e != nil {
		t.Fatal("configuration unavailable")
	}
	profile, ok := config.Profiles["deepseek"]
	if !ok || !isDeepSeekEndpoint(profile.BaseURL) {
		t.Fatal("official DeepSeek configuration required")
	}
	key := strings.TrimSpace(os.Getenv(profile.APIKeyEnv))
	if key == "" {
		key = config.APIToken
	}
	if key == "" {
		t.Fatal("credential unavailable")
	}
	binary := filepath.Join(output, "support-assistant.exe")
	if data, e := exec.Command("go", "build", "-o", binary, "./cmd/support-assistant").CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, data)
	}
	crm, _ := filepath.Abs("../../examples/support-assistant/crm.json")
	docs, _ := filepath.Abs("../../examples/support-assistant/docs")
	state := filepath.Join(output, "state")
	type row struct {
		User     string         `json:"user"`
		Question string         `json:"question"`
		Ticket   string         `json:"ticket_id"`
		HTTP     int            `json:"http_status"`
		Reply    *support.Reply `json:"reply,omitempty"`
	}
	report := struct {
		Kind  string        `json:"kind"`
		Rows  []row         `json:"rows"`
		Usage support.Usage `json:"usage"`
	}{Kind: "Real DeepSeek + real stdio MCP subprocess + local TF-IDF FAQ RAG + authenticated HTTP service"}
	save := func() {
		b, _ := json.MarshalIndent(report, "", "  ")
		os.WriteFile(filepath.Join(output, "report.json"), b, 0600)
	}
	defer save()
	for _, user := range []string{"U-100", "U-200"} {
		func() {
			listener, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			address := listener.Addr().String()
			listener.Close()
			command := exec.Command(binary, "--crm", crm, "--docs", docs, "--user", user, "--state", state, "--listen", address, "--budget-usd", "0.20")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(strings.ToUpper(entry), "DEEPSEEK_API_KEY=") {
					command.Env = append(command.Env, entry)
				}
			}
			command.Env = append(command.Env, "DEEPSEEK_API_KEY="+key)
			stdout, e := command.StdoutPipe()
			if e != nil {
				t.Fatal(e)
			}
			var errors bytes.Buffer
			command.Stderr = &errors
			if e = command.Start(); e != nil {
				t.Fatal("service startup failed")
			}
			finished := make(chan error, 1)
			go func() { finished <- command.Wait() }()
			ready := make(chan bool, 1)
			go func() {
				scanner := bufio.NewScanner(stdout)
				for scanner.Scan() {
					if strings.HasPrefix(scanner.Text(), "Support ready:") {
						ready <- true
						return
					}
				}
				ready <- false
			}()
			token := ""
			client := &http.Client{Timeout: 105 * time.Second}
			request := func(path string, payload []byte) (int, []byte) {
				method := "GET"
				if payload != nil {
					method = "POST"
				}
				req, e := http.NewRequest(method, "http://"+address+path, bytes.NewReader(payload))
				if e != nil {
					t.Fatal(e)
				}
				req.Header.Set("Authorization", "Bearer "+token)
				if payload != nil {
					req.Header.Set("Content-Type", "application/json")
				}
				resp, e := client.Do(req)
				if e != nil {
					t.Fatal("local HTTP request failed")
				}
				defer resp.Body.Close()
				data, e := io.ReadAll(resp.Body)
				if e != nil {
					t.Fatal("local response unreadable")
				}
				return resp.StatusCode, data
			}
			defer func() {
				if token != "" {
					request("/api/shutdown", []byte("{}"))
				}
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					command.Process.Kill()
					<-finished
					t.Error("service did not shut down cleanly")
				}
			}()
			select {
			case active := <-ready:
				if !active {
					t.Fatal("service did not become ready")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("service startup timed out")
			}
			b, e := os.ReadFile(filepath.Join(state, "access-token.txt"))
			if e != nil {
				t.Fatal("local access token missing")
			}
			token = string(b)
			status, data := request("/api/tickets", nil)
			if status != 200 {
				t.Fatal("MCP ticket list unavailable")
			}
			var tickets support.TicketList
			json.Unmarshal(data, &tickets)
			for _, ticket := range tickets.Tickets {
				if ticket.UserID != user {
					t.Fatal("cross-user ticket disclosure")
				}
			}
			cases := []struct {
				question, ticket, contains string
				code                       int
			}{}
			if user == "U-100" {
				cases = append(cases,
					struct {
						question, ticket, contains string
						code                       int
					}{"Почему не работает авторизация?", "T-100", "подтверд", 200},
					struct {
						question, ticket, contains string
						code                       int
					}{"Почему не работает экспорт PDF?", "T-101", "Free", 200},
					struct {
						question, ticket, contains string
						code                       int
					}{"Погода на Юпитере завтра", "", "unknown", 200},
					struct {
						question, ticket, contains string
						code                       int
					}{"Почему не работает авторизация?", "T-200", "", 422})
			} else {
				cases = append(cases, struct {
					question, ticket, contains string
					code                       int
				}{"Почему не работает авторизация?", "T-200", "15", 200})
			}
			for _, test := range cases {
				payload, _ := json.Marshal(support.AskRequest{Question: test.question, TicketID: test.ticket})
				code, data := request("/api/ask", payload)
				item := row{User: user, Question: test.question, Ticket: test.ticket, HTTP: code}
				if code == 200 {
					var reply support.Reply
					if json.Unmarshal(data, &reply) != nil {
						t.Fatal("invalid service reply")
					}
					item.Reply = &reply
				}
				report.Rows = append(report.Rows, item)
				save()
				if code != test.code {
					t.Fatalf("HTTP verification failed for %s/%s: %d; evidence retained", user, test.ticket, code)
				}
				if code != 200 {
					continue
				}
				reply := item.Reply
				if reply.Answer == nil {
					t.Fatal("answer missing")
				}
				if test.contains == "unknown" {
					if reply.Answer.Status != "unknown" || !reply.Answer.Local {
						t.Fatal("unknown case used model or fabricated answer")
					}
				} else {
					if reply.Answer.Status != "answered" || !strings.Contains(strings.ToLower(reply.Answer.Answer), strings.ToLower(test.contains)) {
						t.Fatalf("semantic check failed for %s/%s; evidence retained", user, test.ticket)
					}
					docs, mcp := false, false
					for _, source := range reply.Answer.Sources {
						docs = docs || strings.HasPrefix(source.Source, "faq/")
						mcp = mcp || strings.HasPrefix(source.Source, "mcp://support/")
					}
					if !docs || !mcp {
						t.Fatal("missing documentation or MCP citations")
					}
				}
				t.Logf("%s/%s: %s; provider calls %d", user, test.ticket, reply.Answer.Status, reply.Usage.Calls)
			}
		}()
	}
	b, e := os.ReadFile(filepath.Join(state, "budget.json"))
	if e != nil || json.Unmarshal(b, &report.Usage) != nil {
		t.Fatal("usage ledger unavailable")
	}
	save()
	fmt.Printf("Support live verification: %d cases, %d calls, reserved $%.8f, peak estimate $%.8f\n", len(report.Rows), report.Usage.Calls, report.Usage.Reserved, report.Usage.EstimatedPeak)
}
