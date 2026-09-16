package filetools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocumentsReadAndBoundaries(t *testing.T) {
	d := &Documents{Dir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(d.Dir, "article.txt"), []byte("Code 7392"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	value, err := d.Execute(ctx, "read_file", `{"name":"article.txt"}`)
	if err != nil || !strings.Contains(value, "7392") {
		t.Fatalf("%s %v", value, err)
	}
	for _, args := range []string{`{"name":"../config.json"}`, `{"name":"C:\\secret.txt"}`, `{"name":"sub/file.txt"}`, `{"name":"file.txt:stream"}`, `{"name":".secret.txt"}`, `{"name":"article.txt","extra":1}`, `{} {}`} {
		if _, err := d.Execute(ctx, "read_file", args); err == nil {
			t.Fatal(args)
		}
	}
	if _, err := d.Execute(ctx, "shell", `{}`); err == nil {
		t.Fatal("unknown tool accepted")
	}
	if err := os.WriteFile(filepath.Join(d.Dir, "big.txt"), []byte(strings.Repeat("x", (1<<20)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Execute(ctx, "read_file", `{"name":"big.txt"}`); err == nil {
		t.Fatal("size limit missing")
	}
	if err := os.WriteFile(filepath.Join(d.Dir, "binary.txt"), []byte{0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Execute(ctx, "read_file", `{"name":"binary.txt"}`); err == nil {
		t.Fatal("UTF-8 validation missing")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := d.Execute(cancelled, "list_files", `{}`); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestSymlinkEscape(t *testing.T) {
	d := &Documents{Dir: t.TempDir()}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(d.Dir, "link.txt")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := d.Execute(context.Background(), "read_file", `{"name":"link.txt"}`); err == nil {
		t.Fatal("symlink escape")
	}
}
