package main

import (
	"strings"
	"testing"
)

func TestTokenDemo(t *testing.T) {
	var out strings.Builder
	if code := runTokenDemo(&out); code != 0 {
		t.Fatal(code)
	}
	for _, want := range []string{"1 | 0 | 30 | 37 | 1", "4 | 93 | 30 | 154 | 1", "ПЕРЕПОЛНЕНИЕ", "context_length_exceeded", "8 сообщений (до отказов 8)"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
}
