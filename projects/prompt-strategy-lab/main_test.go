package main

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

func TestConfirmRun(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{input: "y\n", want: true},
		{input: "да\n", want: true},
		{input: "n\n", want: false},
		{input: "\n", want: false},
	}
	for _, test := range tests {
		got, err := confirmRun(bufio.NewReader(strings.NewReader(test.input)), io.Discard)
		if err != nil {
			t.Fatalf("confirmRun(%q) returned error: %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("confirmRun(%q) = %v, want %v", test.input, got, test.want)
		}
	}
}

func TestReadMultiline(t *testing.T) {
	input := "Первая строка\n\nВторая строка с&nbsp;пробелом\n\n\nследующий ввод\n"
	reader := bufio.NewReader(strings.NewReader(input))

	got, err := readMultiline(reader, io.Discard, "prompt")
	if err != nil {
		t.Fatalf("readMultiline() returned error: %v", err)
	}
	if got != "Первая строка\n\nВторая строка с пробелом" {
		t.Fatalf("readMultiline() = %q", got)
	}

	next, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("reading remaining input: %v", err)
	}
	if next != "следующий ввод\n" {
		t.Fatalf("remaining input = %q", next)
	}
}

func TestReadMultilineAllowsEmptyInput(t *testing.T) {
	got, err := readMultiline(bufio.NewReader(strings.NewReader("\n\n")), io.Discard, "prompt")
	if err != nil {
		t.Fatalf("readMultiline() returned error: %v", err)
	}
	if got != "" {
		t.Fatalf("readMultiline() = %q, want empty string", got)
	}
}

func TestNormalizeInputReplacesNonBreakingSpaces(t *testing.T) {
	input := "one\u00a0two &#xA0; three &#160; four&nbsp;five"
	want := "one two   three   four five"
	if got := normalizeInput(input); got != want {
		t.Fatalf("normalizeInput() = %q, want %q", got, want)
	}
}
