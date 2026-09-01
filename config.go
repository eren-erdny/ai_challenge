package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/term"
)

const (
	configDirectoryName = "deepseek-client"
	configFileName      = "config.json"
)

type appConfig struct {
	APIToken string `json:"deepseek_api_token"`
}

func resolveToken(input *bufio.Reader, inputFile *os.File, output io.Writer) (string, error) {
	if token := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")); token != "" {
		return token, nil
	}

	configPath, err := defaultConfigPath()
	if err != nil {
		return "", err
	}

	token, err := loadToken(configPath)
	if err == nil {
		return token, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("прочитать %s: %w", configPath, err)
	}

	fmt.Fprintln(output, "Токен DeepSeek не найден.")
	fmt.Fprintln(output, "Он нужен для отправки запросов в API DeepSeek.")
	token, err = readSecret(input, inputFile, output)
	if err != nil {
		return "", err
	}

	save, err := confirmSave(input, output, configPath)
	if err != nil {
		return "", err
	}
	if save {
		if err := saveToken(configPath, token); err != nil {
			return "", fmt.Errorf("сохранить токен: %w", err)
		}
		fmt.Fprintf(output, "Токен сохранён в %s\n", configPath)
	}

	return token, nil
}

func defaultConfigPath() (string, error) {
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("определить пользовательскую папку конфигурации: %w", err)
	}

	return filepath.Join(configRoot, configDirectoryName, configFileName), nil
}

func loadToken(configPath string) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}

	var config appConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("некорректный JSON: %w", err)
	}

	token := strings.TrimSpace(config.APIToken)
	if token == "" {
		return "", errors.New("поле deepseek_api_token пустое")
	}

	if err := restrictPermissions(configPath, 0o600); err != nil {
		return "", fmt.Errorf("ограничить права конфигурации: %w", err)
	}

	return token, nil
}

func saveToken(configPath string, token string) error {
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("создать папку конфигурации: %w", err)
	}
	if err := restrictPermissions(configDir, 0o700); err != nil {
		return fmt.Errorf("ограничить права папки конфигурации: %w", err)
	}

	data, err := json.MarshalIndent(appConfig{APIToken: token}, "", "  ")
	if err != nil {
		return fmt.Errorf("подготовить конфигурацию: %w", err)
	}
	data = append(data, '\n')

	tempFile, err := os.CreateTemp(configDir, "config-*.tmp")
	if err != nil {
		return fmt.Errorf("создать временный файл: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if err := tempFile.Chmod(0o600); err != nil {
		tempFile.Close()
		return fmt.Errorf("ограничить права временного файла: %w", err)
	}
	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		return fmt.Errorf("записать конфигурацию: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("закрыть конфигурацию: %w", err)
	}

	if err := os.Rename(tempPath, configPath); err != nil {
		return fmt.Errorf("установить конфигурацию: %w", err)
	}

	return nil
}

func restrictPermissions(path string, mode fs.FileMode) error {
	if runtime.GOOS == "windows" {
		// Windows использует ACL пользовательской папки вместо Unix-режимов.
		return nil
	}

	return os.Chmod(path, mode)
}

func readSecret(input *bufio.Reader, inputFile *os.File, output io.Writer) (string, error) {
	fmt.Fprint(output, "Введите API-токен: ")

	var token string
	if term.IsTerminal(int(inputFile.Fd())) {
		secret, err := term.ReadPassword(int(inputFile.Fd()))
		fmt.Fprintln(output)
		if err != nil {
			return "", fmt.Errorf("прочитать скрытый ввод: %w", err)
		}
		token = strings.TrimSpace(string(secret))
		for i := range secret {
			secret[i] = 0
		}
	} else {
		line, err := input.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("прочитать токен: %w", err)
		}
		token = strings.TrimSpace(line)
	}

	if token == "" {
		return "", errors.New("токен не может быть пустым")
	}

	return token, nil
}

func confirmSave(input *bufio.Reader, output io.Writer, configPath string) (bool, error) {
	fmt.Fprintf(output, "Сохранить токен в пользовательской конфигурации %s? [Y/n]: ", configPath)
	answer, err := input.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("прочитать подтверждение: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "y", "yes", "д", "да":
		return true, nil
	case "n", "no", "н", "нет":
		return false, nil
	default:
		return false, errors.New("ожидался ответ Y или n")
	}
}
