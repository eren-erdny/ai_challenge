package mcptools

import (
	"context"
	"errors"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"testing"
)

type fakeSession struct {
	calls, closed int
	name, args    string
	fail          bool
}

func (f *fakeSession) Definitions() []agent.ToolDefinition {
	return []agent.ToolDefinition{{Type: "function", Function: agent.ToolFunction{Name: "echo", Parameters: []byte(`{"type":"object"}`)}}}
}
func (f *fakeSession) Execute(_ context.Context, name, args string) (string, error) {
	f.calls++
	f.name = name
	f.args = args
	if f.fail {
		return "", errors.New("offline")
	}
	return args, nil
}
func (f *fakeSession) Close() error { f.closed++; return nil }
func registration(name string, f *fakeSession) Registration {
	return Registration{Name: name, Connect: func(context.Context) (Session, error) { return f, nil }}
}
func TestRegistryRoutesAndIsolatesFailures(t *testing.T) {
	a, b, local := &fakeSession{}, &fakeSession{}, &fakeSession{}
	r, err := ConnectMany(context.Background(), []Registration{registration("a", a), registration("b", b)}, local)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Definitions()) != 3 {
		t.Fatal("missing definitions")
	}
	for _, name := range []string{"a__echo", "b__echo", "echo"} {
		if got, e := r.Execute(context.Background(), name, `{"x":1}`); e != nil || got != `{"x":1}` {
			t.Fatalf("%s: %s %v", name, got, e)
		}
	}
	if a.name != "echo" || b.name != "echo" || a.calls != 1 || b.calls != 1 || local.calls != 1 {
		t.Fatal("wrong route")
	}
	a.fail = true
	if _, err = r.Execute(context.Background(), "a__echo", "{}"); err == nil {
		t.Fatal("missing error")
	}
	if _, err = r.Execute(context.Background(), "unknown", "{}"); err == nil {
		t.Fatal("unknown accepted")
	}
	if b.calls != 1 || local.calls != 1 {
		t.Fatal("failure rerouted")
	}
	defs := r.Definitions()
	defs[0].Function.Parameters[0] = '!'
	if r.Definitions()[0].Function.Parameters[0] != '{' {
		t.Fatal("mutable definitions")
	}
	r.Close()
	r.Close()
	if a.closed != 1 || b.closed != 1 || local.closed != 0 {
		t.Fatal("wrong session ownership")
	}
	if _, err = r.Execute(context.Background(), "b__echo", "{}"); err == nil {
		t.Fatal("closed execution")
	}
}
func TestRegistrySetupCleanup(t *testing.T) {
	a := &fakeSession{}
	_, err := ConnectMany(context.Background(), []Registration{registration("a", a), {Name: "b", Connect: func(context.Context) (Session, error) { return nil, errors.New("offline") }}}, nil)
	if err == nil || a.closed != 1 {
		t.Fatal("setup leaked session")
	}
	a = &fakeSession{}
	_, err = ConnectMany(context.Background(), []Registration{registration("a", a), registration("a", a)}, nil)
	if err == nil || a.closed != 0 {
		t.Fatal("duplicate registration not rejected before connect")
	}
}
