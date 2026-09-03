package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

type sharedConfig struct {
	APIToken string `json:"deepseek_api_token"`
}

func resolveToken(input *bufio.Reader, inputFile *os.File, output io.Writer) (string, error) {
	if token := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")); token != "" {
		return token, nil
	}

	configRoot, err := os.UserConfigDir()
	if err == nil {
		configPath := filepath.Join(configRoot, "deepseek-client", "config.json")
		if token, err := readTokenFromConfig(configPath); err == nil && token != "" {
			return token, nil
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("прочитать общую конфигурацию %s: %w", configPath, err)
		}
	}

	fmt.Fprintln(output, "Токен не найден в DEEPSEEK_API_KEY или конфигурации deepseek-client.")
	fmt.Fprint(output, "Введите API-токен для текущего запуска: ")
	var token string
	if term.IsTerminal(int(inputFile.Fd())) {
		secret, err := term.ReadPassword(int(inputFile.Fd()))
		fmt.Fprintln(output)
		if err != nil {
			return "", fmt.Errorf("прочитать скрытый ввод: %w", err)
		}
		token = strings.TrimSpace(string(secret))
		for index := range secret {
			secret[index] = 0
		}
	} else {
		line, err := input.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		token = strings.TrimSpace(line)
	}
	if token == "" {
		return "", errors.New("токен не может быть пустым")
	}
	return token, nil
}

func readTokenFromConfig(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var config sharedConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("некорректный JSON: %w", err)
	}
	return strings.TrimSpace(config.APIToken), nil
}
