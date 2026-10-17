package knowledge

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAttachExistingLocalIndex(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "qdrant"), 0700)
	manifest := map[string]any{"backend": "local", "qdrant_path": filepath.Join(dir, "qdrant"), "source_roots": []string{dir}, "collections": map[string]any{"structured": map[string]any{"name": "saved"}}}
	b, _ := json.Marshal(manifest)
	path := filepath.Join(dir, "index.json")
	os.WriteFile(path, b, 0600)
	m := &Manager{Dir: t.TempDir()}
	base, e := m.AttachIndex("Saved", path, "chat")
	if e != nil {
		t.Fatal(e)
	}
	again, e := m.AttachIndex("Saved", path, "other")
	if e != nil || again.ID != base.ID {
		t.Fatal("not idempotent", e)
	}
	reopened := &Manager{Dir: m.Dir}
	selected, e := reopened.Selected("chat")
	if e != nil || selected.IndexDir != dir {
		t.Fatal("not persisted", e)
	}
	binary, _ := os.Executable()
	m.prepare = func(context.Context, io.Writer) (string, error) { return binary, nil }
	os.MkdirAll(filepath.Join(m.Dir, "runtime"), 0700)
	defer m.Close()
	answer, e := m.Search(context.Background(), "chat", "hello")
	if e != nil || !strings.Contains(answer, filepath.Base(dir)) {
		t.Fatal("worker did not open borrowed index", answer, e)
	}
	actual, _ := os.ReadFile(path)
	if string(actual) != string(b) {
		t.Fatal("source manifest changed")
	}
	manifest["backend"] = "https://remote.example"
	b, _ = json.Marshal(manifest)
	os.WriteFile(path, b, 0600)
	if _, e = m.AttachIndex("Remote", path, "chat"); e == nil {
		t.Fatal("remote backend accepted")
	}
}
