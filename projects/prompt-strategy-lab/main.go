package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	input := bufio.NewReader(os.Stdin)
	fmt.Println("Лаборатория промпт-стратегий DeepSeek")

	task, err := readMultiline(input, os.Stdout,
		"\nВведите задачу. Для завершения ввода дважды нажмите Enter:\n")
	if err != nil {
		fmt.Fprintf(os.Stderr, "не удалось прочитать задачу: %v\n", err)
		os.Exit(1)
	}
	if task == "" {
		fmt.Println("Задача не введена, программа завершена.")
		return
	}

	reference, err := readMultiline(input, os.Stdout,
		"\nВведите эталонный ответ, если он известен. Дважды нажмите Enter, чтобы пропустить:\n")
	if err != nil {
		fmt.Fprintf(os.Stderr, "не удалось прочитать эталон: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nЗадача:\n" + task)
	if reference != "" {
		fmt.Println("\nЭталон:\n" + reference)
	} else {
		fmt.Println("\nЭталон не задан: API-судья самостоятельно определит правильное решение.")
	}
	fmt.Println("\nБудет выполнено 6 API-запросов: 4 решения, генерация промпта и сравнение.")
	confirmed, err := confirmRun(input, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "не удалось прочитать подтверждение: %v\n", err)
		os.Exit(1)
	}
	if !confirmed {
		fmt.Println("Эксперимент отменён, API-запросы не выполнялись.")
		return
	}

	token, err := resolveToken(input, os.Stdin, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "не удалось получить токен: %v\n", err)
		waitForEnter(input)
		os.Exit(1)
	}

	client := newDeepSeekClient(token)
	results, judgement, err := runExperiment(os.Stdout, task, reference, client.complete)
	if err != nil {
		fmt.Fprintf(os.Stderr, "эксперимент завершился с ошибкой: %v\n", err)
		waitForEnter(input)
		os.Exit(1)
	}

	printResults(os.Stdout, task, reference, results, judgement)
	waitForEnter(input)
}

func readMultiline(input *bufio.Reader, output io.Writer, prompt string) (string, error) {
	fmt.Fprint(output, prompt)
	var lines []string
	emptyLines := 0
	for {
		line, err := input.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(line) == "" {
			emptyLines++
			if emptyLines == 2 {
				break
			}
		} else {
			emptyLines = 0
		}
		lines = append(lines, line)
		if errors.Is(err, io.EOF) {
			break
		}
	}
	return normalizeInput(strings.Join(lines, "\n")), nil
}

func normalizeInput(value string) string {
	replacer := strings.NewReplacer(
		"\u00a0", " ",
		"&nbsp;", " ",
		"&#160;", " ",
		"&#xA0;", " ",
		"&#xa0;", " ",
	)
	return strings.TrimSpace(replacer.Replace(value))
}

func confirmRun(input *bufio.Reader, output io.Writer) (bool, error) {
	fmt.Fprint(output, "Продолжить? [y/N]: ")
	answer, err := input.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes", "д", "да":
		return true, nil
	default:
		return false, nil
	}
}

func waitForEnter(input *bufio.Reader) {
	fmt.Print("\nНажмите Enter, чтобы закрыть программу...")
	_, _ = input.ReadString('\n')
}
