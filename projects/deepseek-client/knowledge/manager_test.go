package knowledge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The test executable acts as a scripted stdio worker without model/API calls.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "-u" {
		input := bufio.NewScanner(os.Stdin)
		for input.Scan() {
			var value map[string]string
			json.Unmarshal(input.Bytes(), &value)
			if value["query"] == "BLOCK" {
				time.Sleep(time.Hour)
			}
			result := map[string]any{"result": map[string]any{"results": []map[string]string{{"text": value["query"], "source": filepath.Base(os.Getenv("RAG_DATA_DIR"))}}}}
			encoded, _ := json.Marshal(result)
			fmt.Println(string(encoded))
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestPersistentWorkerSwitchingAndCancellation(t *testing.T) {
	manager, source := fixtureManager(t)
	ctx := context.Background()
	a, err := manager.Create(ctx, "A", source, "chat", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	b, err := manager.Create(ctx, "B", source, "other", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manager.prepare = func(context.Context, io.Writer) (string, error) { return binary, nil }
	os.MkdirAll(filepath.Join(manager.Dir, "runtime"), 0700)
	defer manager.Close()
	first, err := manager.Search(ctx, "chat", "hello")
	if err != nil || !strings.Contains(first, a.StorageID) {
		t.Fatalf("wrong first search %s %v", first, err)
	}
	process := manager.worker.Process.Pid
	if _, err = manager.Search(ctx, "chat", "again"); err != nil || manager.worker.Process.Pid != process {
		t.Fatal("worker was reloaded per request")
	}
	if err = manager.Select("chat", b.ID); err != nil {
		t.Fatal(err)
	}
	second, err := manager.Search(ctx, "chat", "second")
	if err != nil || !strings.Contains(second, b.StorageID) || strings.Contains(second, a.StorageID) {
		t.Fatalf("cross-base evidence %s %v", second, err)
	}
	canceled, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if _, err = manager.Search(canceled, "chat", "BLOCK"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation failed: %v", err)
	}
	if manager.worker != nil {
		t.Fatal("canceled worker left running")
	}
	if err = manager.Select("chat", ""); err != nil {
		t.Fatal(err)
	}
	empty, err := manager.Search(ctx, "chat", "question")
	if err != nil || empty != "" {
		t.Fatal("disabled base searched")
	}
}

func fixtureManager(t *testing.T) (*Manager, string) {
	t.Helper()
	source := filepath.Join(t.TempDir(), "docs with spaces")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "a.md"), []byte("# Evidence\nExample"), 0600); err != nil {
		t.Fatal(err)
	}
	m := &Manager{Dir: filepath.Join(t.TempDir(), "knowledge")}
	m.prepare = func(context.Context, io.Writer) (string, error) { return "fixture-python", nil }
	m.Run = func(ctx context.Context, exe string, args, env []string, dir string, out io.Writer) error {
		if len(args) < 4 || args[2] != "index" {
			t.Fatalf("unexpected arguments %v", args)
		}
		if args[3] != source && !strings.Contains(args[3], "docs") {
			t.Fatal("source quoting changed")
		}
		return ctx.Err()
	}
	return m, source
}
func TestIndependentBasesAndChatSelectionsSurviveRestart(t *testing.T) {
	m, source := fixtureManager(t)
	ctx := context.Background()
	a, err := m.Create(ctx, "Работа", source, "chat-a", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Create(ctx, "Учёба", source, "chat-b", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID || a.StorageID == b.StorageID {
		t.Fatal("bases share storage")
	}
	restored := &Manager{Dir: m.Dir}
	first, err := restored.Selected("chat-a")
	if err != nil || first.ID != a.ID {
		t.Fatalf("lost selection %v %v", first, err)
	}
	second, _ := restored.Selected("chat-b")
	if second.ID != b.ID {
		t.Fatal("wrong base after restart")
	}
	blank, _ := restored.Selected("new-chat")
	if blank.ID != "" {
		t.Fatal("new chat inherited unrelated base")
	}
	if err = restored.Select("chat-a", b.ID); err != nil {
		t.Fatal(err)
	}
	if err = restored.Select("chat-b", ""); err != nil {
		t.Fatal(err)
	}
	first, _ = restored.Selected("chat-a")
	second, _ = restored.Selected("chat-b")
	if first.ID != b.ID || second.ID != "" {
		t.Fatal("selection leaked between chats")
	}
}
func TestFailedRebuildPreservesSelectedIndexAndSources(t *testing.T) {
	m, source := fixtureManager(t)
	ctx := context.Background()
	base, err := m.Create(ctx, "Base", source, "chat", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	additional := filepath.Join(t.TempDir(), "additional.txt")
	os.WriteFile(additional, []byte("more evidence"), 0600)
	m.Run = func(context.Context, string, []string, []string, string, io.Writer) error {
		return errors.New("failed index")
	}
	if err = m.Update(ctx, base.ID, additional, io.Discard); err == nil {
		t.Fatal("accepted failed index")
	}
	current, _ := m.Selected("chat")
	if !reflect.DeepEqual(base, current) {
		t.Fatalf("changed active base on failure %+v", current)
	}
	if _, err = m.Create(ctx, "Failed", source, "chat", io.Discard); err == nil {
		t.Fatal("accepted failed create")
	}
	all, _ := m.List()
	if len(all) != 1 {
		t.Fatal("published failed base")
	}
}
func TestAddingAndRemovingSourcesPublishesNewStorage(t *testing.T) {
	m, source := fixtureManager(t)
	ctx := context.Background()
	base, err := m.Create(ctx, "Base", source, "chat", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	another := filepath.Join(t.TempDir(), "docs-other")
	os.Mkdir(another, 0700)
	var argsSeen []string
	m.Run = func(_ context.Context, _ string, args, _ []string, _ string, _ io.Writer) error {
		argsSeen = append([]string(nil), args...)
		return nil
	}
	if err = m.Update(ctx, base.ID, another, io.Discard); err != nil {
		t.Fatal(err)
	}
	updated, _ := m.Selected("chat")
	if updated.StorageID == base.StorageID || len(updated.Sources) != 2 || !reflect.DeepEqual(argsSeen, []string{"-m", "rag", "index", source, "--add-source", another}) {
		t.Fatalf("bad update %+v %v", updated, argsSeen)
	}
	if err = m.RemoveSource(ctx, base.ID, 1, io.Discard); err != nil {
		t.Fatal(err)
	}
	last, _ := m.Selected("chat")
	if !reflect.DeepEqual(last.Sources, []string{another}) {
		t.Fatal("wrong sources")
	}
	if err = m.RemoveSource(ctx, base.ID, 1, io.Discard); err == nil {
		t.Fatal("removed final source")
	}
	if _, err = os.Stat(source); err != nil {
		t.Fatal("deleted original documents")
	}
}
func TestRejectCorruptCatalogAndDuplicateName(t *testing.T) {
	m, source := fixtureManager(t)
	ctx := context.Background()
	if _, err := m.Create(ctx, "Base", source, "c", io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(ctx, "base", source, "c", io.Discard); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err := m.Select("c", "../../outside"); err == nil {
		t.Fatal("unregistered path accepted")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "catalog.json"), []byte(`{"bases":{"../../outside":{"id":"../../outside"}}}`), 0600)
	bad := &Manager{Dir: dir}
	if _, err := bad.List(); err == nil {
		t.Fatal("unsafe catalog accepted")
	}
}
