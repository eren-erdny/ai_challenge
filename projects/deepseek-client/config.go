package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
	"golang.org/x/term"
)

const (
	configDirectoryName = "deepseek-client"
	configFileName      = "config.json"
	defaultFormatName   = "structured_markdown"
	defaultModelName    = "deepseek-v4-flash"
	defaultAPIBaseURL   = "https://api.deepseek.com"
)

type appConfig struct {
	HistoryPolicy   historyConfig         `json:"history"`
	HistoryNotice   string                `json:"-"`
	ConversationID  string                `json:"-"`
	History         agent.HistoryStore    `json:"-"`
	InitialMessages []agent.Message       `json:"-"`
	ActiveProfile   string                `json:"active_profile"`
	Profiles        map[string]apiProfile `json:"profiles"`
	APIToken        string                `json:"-"`
	Generation      generationConfig      `json:"generation"`
	ResponseControl responseControlConfig `json:"response_control"`
}

type historyConfig struct {
	RetentionDays int `json:"retention_days"`
}

func validateHistory(config historyConfig) error {
	if config.RetentionDays < 0 || config.RetentionDays > 106751 {
		return errors.New("history.retention_days должен быть от 0 до 106751")
	}
	return nil
}

type apiProfile struct {
	BaseURL   string `json:"base_url"`
	APIKeyEnv string `json:"api_key_env"`
	Model     string `json:"model"`
}

type generationConfig struct {
	Model       string         `json:"-"`
	Temperature float64        `json:"temperature"`
	Strategy    promptStrategy `json:"strategy"`
}

type modelDefinition struct {
	Name        string
	Description string
}

var modelCatalog = []modelDefinition{
	{Name: "deepseek-v4-flash", Description: "быстрая универсальная текстовая модель"},
	{Name: "deepseek-v4-pro", Description: "более мощная модель для сложных задач"},
}

type responseControlConfig = agent.ControlConfig

type formatDefinition = agent.FormatDefinition

var formatCatalog = agent.Formats()

func defaultAppConfig() appConfig {
	return appConfig{
		HistoryPolicy: historyConfig{RetentionDays: 30},
		ActiveProfile: "deepseek",
		Profiles: map[string]apiProfile{
			"deepseek": {
				BaseURL:   defaultAPIBaseURL + "/v1",
				APIKeyEnv: "DEEPSEEK_API_KEY",
				Model:     defaultModelName,
			},
			"ollama_cloud": {
				BaseURL:   "https://ollama.com/v1",
				APIKeyEnv: "OLLAMA_API_KEY",
				Model:     "gpt-oss:120b",
			},
		},
		Generation: generationConfig{
			Model:       defaultModelName,
			Temperature: 0.7,
			Strategy:    strategyStandard,
		},
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
	configMissing := errors.Is(err, os.ErrNotExist)
	if configMissing {
		config = defaultAppConfig()
	} else if err != nil {
		return appConfig{}, fmt.Errorf("прочитать %s: %w", configPath, err)
	}

	applyAPIEnvironment(&config)
	if token := strings.TrimSpace(config.APIToken); token != "" {
		config.APIToken = token
		return config, nil
	}

	profile, _ := config.activeAPIProfile()
	if strings.TrimSpace(profile.APIKeyEnv) == "" {
		return config, nil
	}

	fmt.Fprintln(output, "API-токен не найден.")
	fmt.Fprintln(output, "Он нужен для выбранного OpenAI-compatible API.")
	token, err := readSecret(input, inputFile, output)
	if err != nil {
		return appConfig{}, err
	}
	config.APIToken = token
	if configMissing {
		if err := saveConfig(configPath, config); err != nil {
			return appConfig{}, fmt.Errorf("сохранить конфигурацию: %w", err)
		}
		fmt.Fprintf(output, "Настройки без токена сохранены в %s\n", configPath)
	}
	fmt.Fprintf(output, "Токен используется только в памяти. Для следующих запусков задайте %s.\n", profile.APIKeyEnv)

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
	var raw struct {
		History        json.RawMessage       `json:"history"`
		ActiveProfile  string                `json:"active_profile"`
		Profiles       map[string]apiProfile `json:"profiles"`
		APIToken       string                `json:"api_token"`
		LegacyAPIToken string                `json:"deepseek_api_token"`
		LegacyAPI      *struct {
			BaseURL string `json:"base_url"`
		} `json:"api"`
		Generation      json.RawMessage `json:"generation"`
		ResponseControl json.RawMessage `json:"response_control"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return appConfig{}, fmt.Errorf("некорректный JSON: %w", err)
	}
	if len(raw.Generation) > 0 {
		var legacyGeneration struct {
			Model       string         `json:"model"`
			Temperature float64        `json:"temperature"`
			Strategy    promptStrategy `json:"strategy"`
		}
		legacyGeneration.Temperature = config.Generation.Temperature
		legacyGeneration.Strategy = config.Generation.Strategy
		legacyGeneration.Model = config.Generation.Model
		if err := json.Unmarshal(raw.Generation, &legacyGeneration); err != nil {
			return appConfig{}, fmt.Errorf("некорректный generation: %w", err)
		}
		config.Generation = generationConfig{
			Model: legacyGeneration.Model, Temperature: legacyGeneration.Temperature, Strategy: legacyGeneration.Strategy,
		}
	}
	if len(raw.History) > 0 {
		if err := json.Unmarshal(raw.History, &config.HistoryPolicy); err != nil {
			return appConfig{}, fmt.Errorf("некорректный history: %w", err)
		}
	}
	if err := validateHistory(config.HistoryPolicy); err != nil {
		return appConfig{}, err
	}
	if len(raw.ResponseControl) > 0 {
		if err := json.Unmarshal(raw.ResponseControl, &config.ResponseControl); err != nil {
			return appConfig{}, fmt.Errorf("некорректный response_control: %w", err)
		}
	}
	if len(raw.Profiles) > 0 {
		config.ActiveProfile = strings.TrimSpace(raw.ActiveProfile)
		config.Profiles = raw.Profiles
	} else {
		baseURL := defaultAPIBaseURL + "/v1"
		if raw.LegacyAPI != nil && strings.TrimSpace(raw.LegacyAPI.BaseURL) != "" {
			baseURL = raw.LegacyAPI.BaseURL
		}
		profileName := "default"
		keyEnv := "OPENAI_API_KEY"
		if isDeepSeekEndpoint(baseURL) {
			profileName = "deepseek"
			keyEnv = "DEEPSEEK_API_KEY"
		}
		config.ActiveProfile = profileName
		config.Profiles = map[string]apiProfile{profileName: {
			BaseURL: baseURL, APIKeyEnv: keyEnv, Model: config.Generation.Model,
		}}
	}
	if raw.APIToken != "" {
		config.APIToken = raw.APIToken
	} else if profile, ok := config.activeAPIProfile(); ok && isDeepSeekEndpoint(profile.BaseURL) {
		config.APIToken = raw.LegacyAPIToken
	}
	if err := config.normalizeAndValidateProfiles(); err != nil {
		return appConfig{}, fmt.Errorf("некорректные profiles: %w", err)
	}
	if err := validateResponseControl(config.ResponseControl); err != nil {
		return appConfig{}, fmt.Errorf("некорректный response_control: %w", err)
	}
	if err := validateGeneration(config.Generation); err != nil {
		return appConfig{}, fmt.Errorf("некорректный generation: %w", err)
	}

	if err := restrictPermissions(configPath, 0o600); err != nil {
		return appConfig{}, fmt.Errorf("ограничить права конфигурации: %w", err)
	}

	return config, nil
}

func loadConfigForReload(configPath string, currentToken string, currentBaseURL string) (appConfig, error) {
	config, err := loadConfig(configPath)
	if err != nil {
		return appConfig{}, err
	}
	applyAPIEnvironment(&config)
	profile, _ := config.activeAPIProfile()
	if strings.TrimSpace(config.APIToken) == "" && sameAPIEndpoint(profile.BaseURL, currentBaseURL) {
		config.APIToken = currentToken
	}
	if profile.APIKeyEnv != "" && strings.TrimSpace(config.APIToken) == "" {
		return appConfig{}, fmt.Errorf("для профиля %q не найдена переменная %s", config.ActiveProfile, profile.APIKeyEnv)
	}
	return config, nil
}

func saveConfig(configPath string, config appConfig) error {
	if err := validateHistory(config.HistoryPolicy); err != nil {
		return err
	}
	if err := config.normalizeAndValidateProfiles(); err != nil {
		return fmt.Errorf("некорректные profiles: %w", err)
	}
	if err := validateGeneration(config.Generation); err != nil {
		return fmt.Errorf("некорректный generation: %w", err)
	}
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

	fileConfig := struct {
		History       historyConfig         `json:"history"`
		ActiveProfile string                `json:"active_profile"`
		Profiles      map[string]apiProfile `json:"profiles"`
		Generation    struct {
			Temperature float64        `json:"temperature"`
			Strategy    promptStrategy `json:"strategy"`
		} `json:"generation"`
		ResponseControl responseControlConfig `json:"response_control"`
	}{
		History:       config.HistoryPolicy,
		ActiveProfile: config.ActiveProfile, Profiles: config.Profiles, ResponseControl: config.ResponseControl,
	}
	fileConfig.Generation.Temperature = config.Generation.Temperature
	fileConfig.Generation.Strategy = config.Generation.Strategy
	data, err := json.MarshalIndent(fileConfig, "", "  ")
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

func validateGeneration(config generationConfig) error {
	if strings.TrimSpace(config.Model) == "" {
		return errors.New("model не может быть пустой")
	}
	if math.IsNaN(config.Temperature) || math.IsInf(config.Temperature, 0) ||
		config.Temperature < 0 || config.Temperature > 2 {
		return errors.New("temperature должна быть в диапазоне от 0 до 2")
	}
	switch config.Strategy {
	case strategyStandard, strategyStepByStep, strategyExperts:
		return nil
	default:
		return fmt.Errorf("неизвестная strategy %q; доступны standard, step_by_step, experts", config.Strategy)
	}
}

func printModelCatalog(output io.Writer) {
	fmt.Fprintln(output, "Встроенные модели DeepSeek:")
	for _, model := range modelCatalog {
		fmt.Fprintf(output, "  %-22s %s\n", model.Name, model.Description)
	}
	fmt.Fprintln(output, "Для OpenAI-compatible API можно указать любое имя через /model NAME или config.json.")
}

func validateAPIProfile(profile apiProfile) error {
	parsed, err := url.Parse(strings.TrimSpace(profile.BaseURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("base_url должен быть корректным http:// или https:// URL")
	}
	if parsed.User != nil {
		return errors.New("base_url не должен содержать логин или пароль")
	}
	if strings.TrimSpace(profile.Model) == "" {
		return errors.New("model не может быть пустой")
	}
	if strings.ContainsAny(profile.APIKeyEnv, " \t\r\n=") {
		return errors.New("api_key_env должен быть именем переменной окружения без пробелов")
	}
	return nil
}

func applyAPIEnvironment(config *appConfig) {
	profile, ok := config.activeAPIProfile()
	if !ok {
		return
	}
	if baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")); baseURL != "" {
		profile.BaseURL = baseURL
		profile.APIKeyEnv = "OPENAI_API_KEY"
	}
	if model := strings.TrimSpace(os.Getenv("OPENAI_MODEL")); model != "" {
		profile.Model = model
	}
	config.Profiles[config.ActiveProfile] = profile
	config.Generation.Model = profile.Model

	if os.Getenv("OPENAI_BASE_URL") != "" {
		config.APIToken = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
		return
	}
	if profile.APIKeyEnv == "" {
		config.APIToken = ""
		return
	}
	if token := strings.TrimSpace(os.Getenv(profile.APIKeyEnv)); token != "" {
		config.APIToken = token
	}
}

func (config *appConfig) normalizeAndValidateProfiles() error {
	config.ActiveProfile = strings.TrimSpace(config.ActiveProfile)
	if config.ActiveProfile == "" {
		return errors.New("active_profile не может быть пустым")
	}
	if len(config.Profiles) == 0 {
		return errors.New("profiles не может быть пустым")
	}
	for name, profile := range config.Profiles {
		if strings.TrimSpace(name) == "" || strings.ContainsAny(name, " \t\r\n") {
			return fmt.Errorf("некорректное имя профиля %q", name)
		}
		profile.BaseURL = strings.TrimRight(strings.TrimSpace(profile.BaseURL), "/")
		profile.APIKeyEnv = strings.TrimSpace(profile.APIKeyEnv)
		profile.Model = strings.TrimSpace(profile.Model)
		if err := validateAPIProfile(profile); err != nil {
			return fmt.Errorf("профиль %q: %w", name, err)
		}
		config.Profiles[name] = profile
	}
	profile, ok := config.activeAPIProfile()
	if !ok {
		return fmt.Errorf("active_profile %q отсутствует в profiles", config.ActiveProfile)
	}
	config.Generation.Model = profile.Model
	return nil
}

func (config appConfig) activeAPIProfile() (apiProfile, bool) {
	profile, ok := config.Profiles[config.ActiveProfile]
	return profile, ok
}

func sameAPIEndpoint(left string, right string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(left), "/"), strings.TrimRight(strings.TrimSpace(right), "/"))
}

func isDeepSeekEndpoint(baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	return err == nil && strings.EqualFold(parsed.Hostname(), "api.deepseek.com")
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
	return agent.ValidateControlConfig(config)
}

func buildResponseControl(config responseControlConfig) (*responseControl, error) {
	return agent.BuildControl(config)
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
