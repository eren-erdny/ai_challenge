package main

import (
	"bufio"
	"strings"
	"testing"
)

func TestReadPrompt(t *testing.T) {
	input := bufio.NewReader(strings.NewReader("Расскажи про интерфейсы в Go\n"))
	prompt, err := readPrompt(input)
	if err != nil {
		t.Fatalf("readPrompt() returned error: %v", err)
	}

	if want := "Расскажи про интерфейсы в Go"; prompt != want {
		t.Fatalf("readPrompt() = %q, want %q", prompt, want)
	}
}

func TestReadPromptRejectsEmptyInput(t *testing.T) {
	input := bufio.NewReader(strings.NewReader("   \n"))
	if _, err := readPrompt(input); err == nil {
		t.Fatal("readPrompt() must reject empty input")
	}
}

func TestWaitForEnter(t *testing.T) {
	input := bufio.NewReader(strings.NewReader("\n"))
	var output strings.Builder

	waitForEnter(input, &output)

	if want := "Нажмите Enter, чтобы закрыть программу"; !strings.Contains(output.String(), want) {
		t.Fatalf("waitForEnter() output = %q, must contain %q", output.String(), want)
	}
}

func TestFormatAnswer(t *testing.T) {
	input := "  ## Go  \r\n\r\n\r\nТекст ответа.   \r\n\r\n- пункт\t\r\n  "
	want := "## Go\n\nТекст ответа.\n\n- пункт"

	if got := formatAnswer(input); got != want {
		t.Fatalf("formatAnswer() = %q, want %q", got, want)
	}
}
