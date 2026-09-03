package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

type sessionMode string

const (
	modeFree       sessionMode = "free"
	modeControlled sessionMode = "controlled"
	modeCompare    sessionMode = "compare"
)

type askFunction func(string, string, *responseControl) (completionResult, error)

type sessionState struct {
	Mode    sessionMode
	Control responseControlConfig
}

func runInteractiveSession(
	input *bufio.Reader,
	output io.Writer,
	errorOutput io.Writer,
	config appConfig,
	ask askFunction,
) int {
	mode := modeControlled
	if !config.ResponseControl.Enabled {
		mode = modeFree
	}
	state := sessionState{Mode: mode, Control: config.ResponseControl}

	printSessionWelcome(output, state)
	for {
		fmt.Fprint(output, "\nВы: ")
		line, err := input.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			fmt.Fprintf(errorOutput, "не удалось прочитать ввод: %v\n", err)
			return 1
		}

		text := strings.TrimSpace(line)
		if text == "" && errors.Is(err, io.EOF) {
			fmt.Fprintln(output, "\nСеанс завершён.")
			return 0
		}
		if text == "" {
			continue
		}
		if text == "/exit" || strings.EqualFold(text, "exit") || strings.EqualFold(text, "выход") {
			fmt.Fprintln(output, "Сеанс завершён.")
			return 0
		}
		if strings.HasPrefix(text, "/") {
			handleSessionCommand(text, &state, output)
			if errors.Is(err, io.EOF) {
				return 0
			}
			continue
		}

		executeQuestion(config.APIToken, text, state, output, errorOutput, ask)
		if errors.Is(err, io.EOF) {
			return 0
		}
	}
}

func printSessionWelcome(output io.Writer, state sessionState) {
	fmt.Fprintln(output, "Добрый день! Интерактивный клиент DeepSeek запущен.")
	fmt.Fprintln(output, "Каждый вопрос отправляется независимо от предыдущих.")
	fmt.Fprintf(output, "Текущий режим: %s; формат: %s\n", state.Mode, state.Control.Format)
	fmt.Fprintln(output, "Команды: /mode, /format, /formats, /settings, /help, /exit")
}

func handleSessionCommand(command string, state *sessionState, output io.Writer) {
	parts := strings.Fields(command)
	switch parts[0] {
	case "/help":
		fmt.Fprintln(output, "/mode free        — один запрос без ограничений")
		fmt.Fprintln(output, "/mode controlled  — один запрос с настройками и локальным судьёй")
		fmt.Fprintln(output, "/mode compare     — два запроса и сравнение")
		fmt.Fprintln(output, "/format NAME      — сменить формат в памяти до закрытия программы")
		fmt.Fprintln(output, "/formats          — показать справочник форматов")
		fmt.Fprintln(output, "/settings         — показать текущие настройки")
		fmt.Fprintln(output, "/exit             — завершить программу")
	case "/mode":
		if len(parts) == 1 {
			fmt.Fprintf(output, "Текущий режим: %s\n", state.Mode)
			return
		}
		mode := sessionMode(parts[1])
		if mode != modeFree && mode != modeControlled && mode != modeCompare {
			fmt.Fprintln(output, "Неизвестный режим. Доступны: free, controlled, compare")
			return
		}
		state.Mode = mode
		fmt.Fprintf(output, "Режим изменён: %s\n", state.Mode)
	case "/format":
		if len(parts) != 2 {
			fmt.Fprintln(output, "Использование: /format NAME")
			return
		}
		candidate := state.Control
		candidate.Enabled = true
		candidate.Format = parts[1]
		if _, err := buildResponseControl(candidate); err != nil {
			fmt.Fprintf(output, "Формат не применён: %v\n", err)
			return
		}
		state.Control.Format = parts[1]
		fmt.Fprintf(output, "Формат изменён: %s\n", state.Control.Format)
	case "/formats":
		printFormatCatalog(output)
	case "/settings":
		printSessionSettings(output, *state)
	default:
		fmt.Fprintln(output, "Неизвестная команда. Используйте /help")
	}
}

func printSessionSettings(output io.Writer, state sessionState) {
	fmt.Fprintf(output, "Режим: %s\n", state.Mode)
	fmt.Fprintf(output, "Формат: %s\n", state.Control.Format)
	fmt.Fprintf(output, "Максимум слов: %d\n", state.Control.MaxWords)
	fmt.Fprintf(output, "Максимум токенов: %d\n", state.Control.MaxTokens)
	fmt.Fprintf(output, "Stop sequences: %q\n", state.Control.StopSequences)
}

func executeQuestion(
	token string,
	prompt string,
	state sessionState,
	output io.Writer,
	errorOutput io.Writer,
	ask askFunction,
) {
	switch state.Mode {
	case modeFree:
		fmt.Fprintln(output, "Обрабатываю запрос без ограничений...")
		answer, err := ask(token, prompt, nil)
		if err != nil {
			fmt.Fprintf(errorOutput, "ошибка запроса: %v\n", err)
			return
		}
		printSingleAnswer(output, "ОТВЕТ БЕЗ ОГРАНИЧЕНИЙ", answer, nil)
	case modeControlled:
		control, err := activeControl(state.Control)
		if err != nil {
			fmt.Fprintf(errorOutput, "ошибка настроек: %v\n", err)
			return
		}
		fmt.Fprintf(output, "Обрабатываю запрос с ограничениями, формат %s...\n", control.Format)
		answer, err := ask(token, prompt, control)
		if err != nil {
			fmt.Fprintf(errorOutput, "ошибка запроса: %v\n", err)
			return
		}
		printSingleAnswer(output, "ОТВЕТ С ОГРАНИЧЕНИЯМИ", answer, control)
	case modeCompare:
		control, err := activeControl(state.Control)
		if err != nil {
			fmt.Fprintf(errorOutput, "ошибка настроек: %v\n", err)
			return
		}
		fmt.Fprintln(output, "[1/2] Получаю ответ без ограничений...")
		uncontrolled, err := ask(token, prompt, nil)
		if err != nil {
			fmt.Fprintf(errorOutput, "ошибка запроса без ограничений: %v\n", err)
			return
		}
		fmt.Fprintf(output, "[2/2] Получаю ответ с ограничениями, формат %s...\n", control.Format)
		controlled, err := ask(token, prompt, control)
		if err != nil {
			fmt.Fprintf(errorOutput, "ошибка запроса с ограничениями: %v\n", err)
			return
		}
		printComparison(output, uncontrolled, controlled, control)
	}
}

func activeControl(config responseControlConfig) (*responseControl, error) {
	config.Enabled = true
	return buildResponseControl(config)
}

func printSingleAnswer(output io.Writer, title string, answer completionResult, control *responseControl) {
	fmt.Fprintln(output, "\n========================================")
	fmt.Fprintln(output, title)
	if control != nil {
		fmt.Fprintf(output, "Формат: %s; максимум слов: %d; max_tokens=%d\n", control.Format, control.MaxWords, control.MaxTokens)
	}
	fmt.Fprintln(output, "========================================")
	fmt.Fprintln(output, formatAnswer(answer.Content))
	fmt.Fprintf(output, "\nМетрики: %d слов, %d символов, %d токенов, finish_reason=%s\n",
		wordCount(answer.Content), utf8.RuneCountInString(answer.Content),
		answer.CompletionTokens, answer.FinishReason)
	if control != nil {
		printValidation(output, validateAnswer(answer, control))
	}
}
