package filetools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefinitionsExposeWorkspaceTools(t *testing.T) {
	definitions := (&Documents{}).Definitions()
	want := []string{"list_files", "read_file", "create_directory", "write_file", "edit_file", "find_files", "search_text"}
	if len(definitions) != len(want) {
		t.Fatalf("definitions=%d", len(definitions))
	}
	for i, name := range want {
		if definitions[i].Function.Name != name || !json.Valid(definitions[i].Function.Parameters) {
			t.Fatalf("definition %d=%+v", i, definitions[i])
		}
	}
}

func TestEditFindSearchAndReadRange(t *testing.T) {
	d := &Documents{Dir: t.TempDir()}
	ctx := context.Background()
	if _, err := d.Execute(ctx, "create_directory", `{"path":"src/main/kotlin"}`); err != nil {
		t.Fatal(err)
	}
	content := "package demo\n\ndata class User(val id: Long)\nfun label() = \"USER\"\n"
	args, _ := json.Marshal(map[string]any{"path": "src/main/kotlin/User.kt", "content": content})
	if _, err := d.Execute(ctx, "write_file", string(args)); err != nil {
		t.Fatal(err)
	}

	edit, _ := json.Marshal(map[string]string{
		"path": "src/main/kotlin/User.kt", "old_text": "data class User(val id: Long)", "new_text": "data class User(val id: Long, val name: String)",
	})
	if result, err := d.Execute(ctx, "edit_file", string(edit)); err != nil || !strings.Contains(result, `"replacements":1`) {
		t.Fatalf("edit=%s err=%v", result, err)
	}
	read, err := d.Execute(ctx, "read_file", `{"path":"src/main/kotlin/User.kt","offset":3,"limit":1}`)
	if err != nil || !strings.Contains(read, "val name: String") || !strings.Contains(read, `"start_line":3`) || !strings.Contains(read, `"truncated":true`) {
		t.Fatalf("range=%s err=%v", read, err)
	}
	found, err := d.Execute(ctx, "find_files", `{"pattern":"**/*.kt"}`)
	if err != nil || !strings.Contains(found, "src/main/kotlin/User.kt") {
		t.Fatalf("find=%s err=%v", found, err)
	}
	searched, err := d.Execute(ctx, "search_text", `{"query":"user","path":"src","case_sensitive":false}`)
	if err != nil || !strings.Contains(searched, `"line":3`) || !strings.Contains(searched, `"line":4`) {
		t.Fatalf("search=%s err=%v", searched, err)
	}
	regex, err := d.Execute(ctx, "search_text", `{"query":"User\\(val id: Long, val name: String\\)","regex":true}`)
	if err != nil || !strings.Contains(regex, "User.kt") {
		t.Fatalf("regex=%s err=%v", regex, err)
	}
}

func TestEditRequiresUniqueExactMatch(t *testing.T) {
	d := &Documents{Dir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(d.Dir, "App.kt"), []byte("same\nsame\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := d.Execute(ctx, "edit_file", `{"path":"App.kt","old_text":"same","new_text":"changed"}`); err == nil || !strings.Contains(err.Error(), "exactly once") {
		t.Fatalf("duplicate edit accepted: %v", err)
	}
	if _, err := d.Execute(ctx, "edit_file", `{"path":"App.kt","old_text":"missing","new_text":"changed"}`); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing edit accepted: %v", err)
	}
	if _, err := d.Execute(ctx, "search_text", `{"query":"[","regex":true}`); err == nil {
		t.Fatal("invalid regex accepted")
	}
}

func TestCreateWriteReadAndListKotlinSource(t *testing.T) {
	d := &Documents{Dir: t.TempDir()}
	ctx := context.Background()
	directory := "src/main/kotlin/com/example/domain"
	created, err := d.Execute(ctx, "create_directory", `{"path":"`+directory+`"}`)
	if err != nil || !strings.Contains(created, `"created":true`) {
		t.Fatalf("create=%s err=%v", created, err)
	}
	created, err = d.Execute(ctx, "create_directory", `{"path":"`+directory+`"}`)
	if err != nil || !strings.Contains(created, `"created":false`) {
		t.Fatalf("idempotent create=%s err=%v", created, err)
	}

	content := "package com.example.domain\n\ndata class User(val id: Long)\n"
	args, _ := json.Marshal(map[string]any{"path": directory + "/User.kt", "content": content})
	written, err := d.Execute(ctx, "write_file", string(args))
	if err != nil || !strings.Contains(written, `"overwritten":false`) {
		t.Fatalf("write=%s err=%v", written, err)
	}
	read, err := d.Execute(ctx, "read_file", `{"path":"`+directory+`/User.kt"}`)
	if err != nil || !strings.Contains(read, "data class User") {
		t.Fatalf("read=%s err=%v", read, err)
	}
	listed, err := d.Execute(ctx, "list_files", `{"path":"`+directory+`"}`)
	if err != nil || !strings.Contains(listed, `"path":"`+directory+`/User.kt"`) || !strings.Contains(listed, `"type":"file"`) {
		t.Fatalf("list=%s err=%v", listed, err)
	}
	root, err := d.Execute(ctx, "list_files", `{}`)
	if err != nil || !strings.Contains(root, `"path":"src"`) || !strings.Contains(root, `"type":"directory"`) {
		t.Fatalf("root list=%s err=%v", root, err)
	}
	entries, err := os.ReadDir(d.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".write-") {
			t.Fatalf("temporary file leaked: %s", entry.Name())
		}
	}
}

func TestWriteRequiresExplicitOverwriteAndExistingParent(t *testing.T) {
	d := &Documents{Dir: t.TempDir()}
	ctx := context.Background()
	if _, err := d.Execute(ctx, "create_directory", `{"path":"src"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Execute(ctx, "write_file", `{"path":"src/App.kt","content":"class App"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Execute(ctx, "write_file", `{"path":"src/App.kt","content":"class Changed"}`); err == nil || !strings.Contains(err.Error(), "overwrite") {
		t.Fatalf("overwrite without consent: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(d.Dir, "src", "App.kt"))
	if string(data) != "class App" {
		t.Fatalf("original changed: %q", data)
	}
	if _, err := d.Execute(ctx, "write_file", `{"path":"src/App.kt","content":"class Changed","overwrite":true}`); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(d.Dir, "src", "App.kt"))
	if string(data) != "class Changed" {
		t.Fatalf("overwrite failed: %q", data)
	}
	if _, err := d.Execute(ctx, "write_file", `{"path":"missing/App.kt","content":"class App"}`); err == nil {
		t.Fatal("missing parent accepted")
	}
}

func TestWorkspaceBoundariesAndValidation(t *testing.T) {
	d := &Documents{Dir: t.TempDir()}
	ctx := context.Background()
	invalid := []string{"../escape", "/absolute", `C:\\absolute`, "src/../escape", ".hidden", "src/.hidden", "file:stream", "NUL", strings.Repeat("a/", 16) + "b"}
	for _, value := range invalid {
		args, _ := json.Marshal(map[string]string{"path": value})
		if _, err := d.Execute(ctx, "create_directory", string(args)); err == nil {
			t.Errorf("unsafe directory accepted: %q", value)
		}
	}
	for _, args := range []string{
		`{"path":"Main.exe","content":"binary"}`,
		`{"path":"../Main.kt","content":"class Main"}`,
		`{"path":"Main.kt"}`,
		`{"path":"Main.kt","content":"x","extra":1}`,
		`{} {}`,
	} {
		if _, err := d.Execute(ctx, "write_file", args); err == nil {
			t.Errorf("invalid write accepted: %s", args)
		}
	}
	large, _ := json.Marshal(map[string]string{"path": "Big.kt", "content": strings.Repeat("x", maxFileSize+1)})
	if _, err := d.Execute(ctx, "write_file", string(large)); err == nil {
		t.Fatal("size limit missing")
	}
	if _, err := d.Execute(ctx, "write_file", `{"path":"Binary.kt","content":"\u0000"}`); err == nil {
		t.Fatal("NUL content accepted")
	}
	if _, err := d.Execute(ctx, "shell", `{}`); err == nil {
		t.Fatal("unknown tool accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := d.Execute(cancelled, "list_files", `{}`); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestReadCompatibilityAndFileValidation(t *testing.T) {
	d := &Documents{Dir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(d.Dir, "article.txt"), []byte("Code 7392"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	value, err := d.Execute(ctx, "read_file", `{"name":"article.txt"}`)
	if err != nil || !strings.Contains(value, "7392") {
		t.Fatalf("%s %v", value, err)
	}
	if err := os.WriteFile(filepath.Join(d.Dir, "big.txt"), []byte(strings.Repeat("x", maxFileSize+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Execute(ctx, "read_file", `{"path":"big.txt"}`); err == nil {
		t.Fatal("size limit missing")
	}
	if err := os.WriteFile(filepath.Join(d.Dir, "binary.txt"), []byte{0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Execute(ctx, "read_file", `{"path":"binary.txt"}`); err == nil {
		t.Fatal("UTF-8 validation missing")
	}
}

func TestSymlinkPathsAreRejected(t *testing.T) {
	d := &Documents{Dir: t.TempDir()}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.kt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(d.Dir, "linked")); err != nil {
		t.Skip("symlinks unavailable")
	}
	ctx := context.Background()
	for tool, args := range map[string]string{
		"list_files":       `{"path":"linked"}`,
		"read_file":        `{"path":"linked/secret.kt"}`,
		"create_directory": `{"path":"linked/new"}`,
		"write_file":       `{"path":"linked/New.kt","content":"class New"}`,
		"edit_file":        `{"path":"linked/secret.kt","old_text":"secret","new_text":"changed"}`,
		"find_files":       `{"path":"linked","pattern":"*.kt"}`,
		"search_text":      `{"path":"linked","query":"secret"}`,
	} {
		if _, err := d.Execute(ctx, tool, args); err == nil {
			t.Errorf("%s followed symlink", tool)
		}
	}
}
