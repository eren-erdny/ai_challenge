package main

import "testing"

func TestDecodeMCPRegistry(t *testing.T) {
	good := `{"servers":[{"name":"github","transport":"builtin-github"},{"name":"git","transport":"http","url":"http://127.0.0.1:8080/mcp","token_env":"GIT_MCP_TOKEN"}]}`
	c, e := decodeMCPRegistry([]byte(good))
	if e != nil || len(c.Servers) != 2 {
		t.Fatalf("%+v %v", c, e)
	}
	for _, s := range []string{
		`{"servers":[]}`,
		good + `{}`,
		`{"servers":[{"name":"a","transport":"shell"}]}`,
		`{"servers":[{"name":"a","transport":"http","url":"https://example.invalid","token":"secret"}]}`,
		`{"servers":[{"name":"a","transport":"builtin-github"},{"name":"a","transport":"builtin-github"}]}`,
		`{"servers":[{"name":"Bad_Name","transport":"builtin-github"}]}`,
	} {
		if _, e := decodeMCPRegistry([]byte(s)); e == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}
func TestDefaultMCPRegistry(t *testing.T) {
	t.Setenv("MCP_SERVERS_FILE", "")
	t.Setenv("GIT_MCP_URL", "http://127.0.0.1:8080/mcp")
	c, e := loadMCPRegistry()
	if e != nil || len(c.Servers) != 2 || c.Servers[0].Name != "github" || c.Servers[1].Name != "git" {
		t.Fatalf("%+v %v", c, e)
	}
}
