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
	defaultFormatName   = "structured_markdown"
)

type appConfig struct {
	APIToken        string                `json:"deepseek_api_token"`
	ResponseControl responseControlConfig `json:"response_control"`
}

type responseControlConfig struct {
	Enabled           bool     `json:"enabled"`
	Format            string   `json:"format"`
	CustomInstruction string   `json:"custom_instruction,omitempty"`
	MaxWords          int      `json:"max_words"`
	MaxTokens         int      `json:"max_tokens"`
	StopSequences     []string `json:"stop_sequences"`
}

type formatDefinition struct {
	Name        string
	Description string
	Instruction string
}

var formatCatalog = []formatDefinition{
	{
		Name:        "plain_text",
		Description: "обычный связный текст с короткими абзацами",
		Instruction: "Используй обычный связный текст и короткие абзацы без Markdown-заголовков.",
	},
	{
		Name:        "short_answer",
		Description: "один короткий абзац без вступления",
		Instruction: "Дай один короткий содержательный абзац без заголовка и вступления.",
	},
	{
		Name:        "bullet_list",
		Description: "краткий маркированный список",
		Instruction: "Верни только краткий маркированный Markdown-список без вступления и заключения.",
	},
	{
		Name:        "structured_markdown",
		Description: "краткий ответ с заголовком и тремя пунктами",
		Instruction: "Используй Markdown: раздел «## Краткий ответ» с одним абзацем, " +
			"затем раздел «## Ключевые пункты» ровно с тремя пунктами маркированного списка.",
	},
	{
		Name:        "json",
		Description: "валидный JSON с полями summary и points",
		Instruction: "Верни валидный JSON без Markdown-обёртки: объект с полем summary типа string " +
			"и полем points типа array of strings.",
	},
	{
		Name:        "custom",
		Description: "пользовательская инструкция из custom_instruction",
	},
}

func defaultAppConfig() appConfig {
	return appConfig{
		ResponseControl: responseControlConfig{
			Enabled:       true,
			Format:        defaultFormatName,
			MaxWords:      80,
			MaxTokens:     200,
			StopSequences: []string{"<END>"},
		},
	}
}

func resolveConfig(input *bufio.Reader, inputFile *os.File, output io.Writer) (appConfig, error) {
	configPath, err := defaultConfigPath()
	if err != nil {
		return appConfig{}, err
	}

	config, err := loadConfig(configPath)
	if errors.Is(err, os.ErrNotExist) {
		config = defaultAppConfig()
	} else if err != nil {
		return appConfig{}, fmt.Errorf("прочитать %s: %w", configPath, err)
	}

	if token := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")); token != "" {
		config.APIToken = token
		return config, nil
	}

	if token := strings.TrimSpace(config.APIToken); token != "" {
		config.APIToken = token
		return config, nil
	}

	fmt.Fprintln(output, "Токен DeepSeek не найден.")
	fmt.Fprintln(output, "Он нужен для отправки запросов в API DeepSeek.")
	token, err := readSecret(input, inputFile, output)
	if err != nil {
		return appConfig{}, err
	}
	config.APIToken = token

	save, err := confirmSave(input, output, configPath)
	if err != nil {
		return appConfig{}, err
	}
	if save {
		if err := saveConfig(configPath, config); err != nil {
			return appConfig{}, fmt.Errorf("сохранить конфигурацию: %w", err)
		}
		fmt.Fprintf(output, "Токен и настройки сохранены в %s\n", configPath)
	}

	return config, nil
}

func defaultConfigPath() (string, error) {
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("определить пользовательскую папку конфигурации: %w", err)
	}

	return filepath.Join(configRoot, configDirectoryName, configFileName), nil
}

func loadConfig(configPath string) (appConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return appConfig{}, err
	}

	config := defaultAppConfig()
	if err := json.Unmarshal(data, &config); err != nil {
		return appConfig{}, fmt.Errorf("некорректный JSON: %w", err)
	}
	if err := validateResponseControl(config.ResponseControl); err != nil {
		return appConfig{}, fmt.Errorf("некорректный response_control: %w", err)
	}

	if err := restrictPermissions(configPath, 0o600); err != nil {
		return appConfig{}, fmt.Errorf("ограничить права конфигурации: %w", err)
	}

	return config, nil
}

func saveConfig(configPath string, config appConfig) error {
	if err := validateResponseControl(config.ResponseControl); err != nil {
		return fmt.Errorf("некорректный response_control: %w", err)
	}

	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("создать папку конфигурации: %w", err)
	}
	if err := restrictPermissions(configDir, 0o700); err != nil {
		return fmt.Errorf("ограничить права папки конфигурации: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
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

	if err := replaceConfigFile(tempPath, configPath); err != nil {
		return fmt.Errorf("установить конфигурацию: %w", err)
	}

	return nil
}

func replaceConfigFile(tempPath string, configPath string) error {
	if err := os.Rename(tempPath, configPath); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		return err
	}

	if err := os.Remove(configPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(tempPath, configPath)
}

func validateResponseControl(config responseControlConfig) error {
	if !config.Enabled {
		return nil
	}
	if config.MaxWords <= 0 {
		return errors.New("max_words должен быть больше нуля")
	}
	if config.MaxTokens <= 0 {
		return errors.New("max_tokens должен быть больше нуля")
	}
	if len(config.StopSequences) > 16 {
		return errors.New("stop_sequences может содержать не более 16 значений")
	}
	for _, sequence := range config.StopSequences {
		if strings.TrimSpace(sequence) == "" {
			return errors.New("stop_sequences не может содержать пустые значения")
		}
	}

	definition, ok := findFormat(config.Format)
	if !ok {
		return fmt.Errorf("неизвестный формат %q; используйте --list-formats", config.Format)
	}
	if definition.Name == "custom" && strings.TrimSpace(config.CustomInstruction) == "" {
		return errors.New("для формата custom заполните custom_instruction")
	}

	return nil
}

func findFormat(name string) (formatDefinition, bool) {
	for _, definition := range formatCatalog {
		if definition.Name == name {
			return definition, true
		}
	}
	return formatDefinition{}, false
}

func buildResponseControl(config responseControlConfig) (*responseControl, error) {
	if !config.Enabled {
		return nil, nil
	}
	if err := validateResponseControl(config); err != nil {
		return nil, err
	}

	definition, _ := findFormat(config.Format)
	formatInstruction := definition.Instruction
	if definition.Name == "custom" {
		formatInstruction = strings.TrimSpace(config.CustomInstruction)
	}

	systemPrompt := fmt.Sprintf(
		"Ответь на русском языке. %s Общий объём ответа — не более %d слов.",
		formatInstruction,
		config.MaxWords,
	)
	if len(config.StopSequences) > 0 {
		systemPrompt += fmt.Sprintf(
			" После содержательной части выведи отдельной строкой %s и сразу заверши ответ.",
			config.StopSequences[0],
		)
	}

	return &responseControl{
		Format:       config.Format,
		SystemPrompt: systemPrompt,
		MaxWords:     config.MaxWords,
		MaxTokens:    config.MaxTokens,
		Stop:         append([]string(nil), config.StopSequences...),
	}, nil
}

func printFormatCatalog(output io.Writer) {
	fmt.Fprintln(output, "Доступные форматы ответа:")
	for _, definition := range formatCatalog {
		fmt.Fprintf(output, "  %-22s %s\n", definition.Name, definition.Description)
	}
}

func restrictPermissions(path string, mode fs.FileMode) error {
	if runtime.GOOS == "windows" {
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
	fmt.Fprintf(output, "Сохранить токен и настройки в %s? [Y/n]: ", configPath)
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
