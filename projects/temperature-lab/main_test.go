package main

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

func TestReadMultilinePreservesSingleEmptyLine(t *testing.T) {
	input := "Первая строка\n\nВторая строка с&nbsp;пробелом\n\n\nnext\n"
	reader := bufio.NewReader(strings.NewReader(input))
	got, err := readMultiline(reader, io.Discard, "prompt")
	if err != nil {
		t.Fatalf("readMultiline() returned error: %v", err)
	}
	if got != "Первая строка\n\nВторая строка с пробелом" {
		t.Fatalf("readMultiline() = %q", got)
	}
	next, err := reader.ReadString('\n')
	if err != nil || next != "next\n" {
		t.Fatalf("remaining input = %q, error = %v", next, err)
	}
}

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
