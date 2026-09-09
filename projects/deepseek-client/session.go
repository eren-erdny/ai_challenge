package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

type sessionMode = agent.Mode
type promptStrategy = agent.Strategy

const (
	modeFree                 sessionMode = agent.Free
	modeControlled           sessionMode = agent.Controlled
	modeCompare              sessionMode = agent.Compare
	modeTemperatureBenchmark sessionMode = agent.TemperatureBenchmark
	modeModelBenchmark       sessionMode = agent.ModelBenchmark
)

const (
	strategyStandard   promptStrategy = agent.Standard
	strategyStepByStep promptStrategy = agent.StepByStep
	strategyExperts    promptStrategy = agent.Experts
)

type requestSettings = agent.Settings

type askFunction func(context.Context, string, string, requestSettings) (completionResult, error)

type sessionState struct {
	ConversationID string
	History        agent.HistoryStore
	Mode           sessionMode
	ActiveProfile  string
	Profiles       map[string]apiProfile
	API            apiProfile
	APIToken       string
	Model          string
	Temperature    float64
	Strategy       promptStrategy
	Control        responseControlConfig
	LastRequest    *requestStatus
}

type requestStatus struct {
	Profile string
	BaseURL string
	Result  completionResult
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
	profile, _ := config.activeAPIProfile()
	state := sessionState{
		ConversationID: conversationID(config.ConversationID),
		History:        config.History,
		Mode:           mode,
		ActiveProfile:  config.ActiveProfile,
		Profiles:       config.Profiles,
		API:            profile,
		APIToken:       config.APIToken,
		Model:          profile.Model,
		Temperature:    config.Generation.Temperature,
		Strategy:       config.Generation.Strategy,
		Control:        config.ResponseControl,
	}

	defer func() { printConversationExit(output, state) }()
	printSessionWelcome(output, state)
	if config.HistoryNotice != "" {
		fmt.Fprintln(output, config.HistoryNotice)
	}
	printConversationMessages(output, config.InitialMessages)
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
			if handled, changed, messages := handleConversationCommand(context.Background(), text, &state, output); handled {
				if changed {
					printConversationMessages(output, messages)
				}
				if errors.Is(err, io.EOF) {
					return 0
				}
				continue
			}
			if text == "/models" {
				ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
				models, modelsErr := fetchModels(ctx, state.APIToken, state.API)
				interrupted := ctx.Err() != nil
				cancel()
				if interrupted {
					return 0
				}
				if modelsErr != nil {
					fmt.Fprintf(errorOutput, "не удалось получить модели: %v\n", modelsErr)
				} else {
					fmt.Fprintln(output, "Модели активного API:")
					fmt.Fprintln(output, strings.Join(models, "\n"))
				}
				continue
			}
			if text == "/reload" {
				newToken, configPath, reloadErr := reloadSessionConfig(state.APIToken, &state)
				if reloadErr != nil {
					fmt.Fprintf(errorOutput, "не удалось перезагрузить конфигурацию: %v\n", reloadErr)
				} else {
					state.APIToken = newToken
					fmt.Fprintf(output, "Конфигурация перезагружена: %s\n", configPath)
					printSessionSettings(output, state)
				}
				continue
			}
			handleSessionCommand(text, &state, output)
			if errors.Is(err, io.EOF) {
				return 0
			}
			continue
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		if status := executeQuestion(ctx, state.APIToken, text, state, output, errorOutput, ask); status != nil {
			state.LastRequest = status
		}
		interrupted := ctx.Err() != nil
		cancel()
		if interrupted {
			return 0
		}
		if errors.Is(err, io.EOF) {
			return 0
		}
	}
}

func printSessionWelcome(output io.Writer, state sessionState) {
	fmt.Fprintln(output, "Добрый день! Интерактивный клиент DeepSeek запущен.")
	printConversationExit(output, state)
	fmt.Fprintf(output, "Профиль: %s; модель: %s; режим: %s; стратегия: %s; temperature: %g; формат: %s\n",
		state.ActiveProfile, state.Model, state.Mode, state.Strategy, state.Temperature, state.Control.Format)
	fmt.Fprintln(output, "Команды: /new, /conversation, /profile, /profiles, /model, /models, /mode, /strategy, /temperature, /format, /status, /reload, /settings, /help, /exit")
}

func handleSessionCommand(command string, state *sessionState, output io.Writer) {
	parts := strings.Fields(command)
	switch parts[0] {
	case "/help":
		fmt.Fprintln(output, "/new              — начать новый чистый диалог")
		fmt.Fprintln(output, "/conversation [ID] — показать ID или открыть сохранённый диалог")
		fmt.Fprintln(output, "/mode free        — один запрос без ограничений")
		fmt.Fprintln(output, "/mode controlled  — один запрос с настройками и локальным судьёй")
		fmt.Fprintln(output, "/mode compare     — два запроса и сравнение")
		fmt.Fprintln(output, "/mode temperature_benchmark — три коротких ответа при temperature 0, 1.2 и 2")
		fmt.Fprintln(output, "/mode model_benchmark — один запрос к DeepSeek Flash, DeepSeek Pro и Ollama Cloud")
		fmt.Fprintln(output, "/strategy standard|step_by_step|experts — стратегия промпта")
		fmt.Fprintln(output, "/temperature N    — температура от 0 до 2")
		fmt.Fprintln(output, "/profile NAME     — переключить профиль API")
		fmt.Fprintln(output, "/profiles         — показать настроенные профили")
		fmt.Fprintln(output, "/model NAME       — выбрать модель API")
		fmt.Fprintln(output, "/models           — получить модели активного API через GET /models")
		fmt.Fprintln(output, "/format NAME      — сменить формат в памяти до закрытия программы")
		fmt.Fprintln(output, "/formats          — показать справочник форматов")
		fmt.Fprintln(output, "/settings         — показать текущие настройки")
		fmt.Fprintln(output, "/status           — токены, стоимость и скорость последнего API-запроса")
		fmt.Fprintln(output, "/reload           — перечитать пользовательский config.json")
		fmt.Fprintln(output, "/clear            — очистить историю TUI")
		fmt.Fprintln(output, "/exit             — завершить программу")
	case "/mode":
		if len(parts) == 1 {
			fmt.Fprintf(output, "Текущий режим: %s\n", state.Mode)
			return
		}
		mode := sessionMode(parts[1])
		if mode == "benchmark" || mode == "benchmark_temperature" {
			mode = modeTemperatureBenchmark
		}
		if mode != modeFree && mode != modeControlled && mode != modeCompare && mode != modeTemperatureBenchmark && mode != modeModelBenchmark {
			fmt.Fprintln(output, "Неизвестный режим. Доступны: free, controlled, compare, temperature_benchmark, model_benchmark")
			return
		}
		state.Mode = mode
		switch state.Mode {
		case modeFree:
			state.Control.Enabled = false
		case modeControlled, modeCompare:
			state.Control.Enabled = true
		}
		fmt.Fprintf(output, "Режим изменён: %s\n", state.Mode)
		if state.Mode == modeTemperatureBenchmark {
			fmt.Fprintln(output, "Будет выполнено 4 API-запроса: 3 коротких ответа и итоговый анализ модели")
		}
		if state.Mode == modeModelBenchmark {
			fmt.Fprintln(output, "Будет выполнено 4 API-запроса: 3 модели и итоговый анализ DeepSeek Pro")
		}
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
		state.Control.Enabled = true
		fmt.Fprintf(output, "Формат изменён: %s\n", state.Control.Format)
	case "/strategy":
		if len(parts) == 1 {
			fmt.Fprintf(output, "Текущая стратегия: %s\n", state.Strategy)
			return
		}
		if len(parts) != 2 {
			fmt.Fprintln(output, "Использование: /strategy standard|step_by_step|experts")
			return
		}
		strategy := promptStrategy(parts[1])
		if err := validateGeneration(generationConfig{Model: state.Model, Temperature: state.Temperature, Strategy: strategy}); err != nil {
			fmt.Fprintf(output, "Стратегия не применена: %v\n", err)
			return
		}
		state.Strategy = strategy
		fmt.Fprintf(output, "Стратегия изменена: %s\n", state.Strategy)
	case "/temperature":
		if len(parts) == 1 {
			fmt.Fprintf(output, "Текущая temperature: %g\n", state.Temperature)
			return
		}
		if len(parts) != 2 {
			fmt.Fprintln(output, "Использование: /temperature NUMBER")
			return
		}
		temperature, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			fmt.Fprintln(output, "Temperature не применена: ожидалось число от 0 до 2")
			return
		}
		if err := validateGeneration(generationConfig{Model: state.Model, Temperature: temperature, Strategy: state.Strategy}); err != nil {
			fmt.Fprintf(output, "Temperature не применена: %v\n", err)
			return
		}
		state.Temperature = temperature
		fmt.Fprintf(output, "Temperature изменена: %g\n", state.Temperature)
	case "/model":
		if len(parts) == 1 {
			fmt.Fprintf(output, "Текущая модель: %s\n", state.Model)
			return
		}
		if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
			fmt.Fprintln(output, "Модель не применена. Использование: /model NAME")
			return
		}
		state.Model = parts[1]
		profile := state.API
		profile.Model = state.Model
		state.API = profile
		state.Profiles[state.ActiveProfile] = profile
		fmt.Fprintf(output, "Модель изменена: %s\n", state.Model)
	case "/profile":
		if len(parts) == 1 {
			fmt.Fprintf(output, "Текущий профиль: %s\n", state.ActiveProfile)
			return
		}
		if len(parts) != 2 {
			fmt.Fprintln(output, "Использование: /profile NAME")
			return
		}
		if err := activateSessionProfile(state, parts[1]); err != nil {
			fmt.Fprintf(output, "Профиль не переключён: %v\n", err)
			return
		}
		fmt.Fprintf(output, "Профиль изменён: %s; model=%s\n", state.ActiveProfile, state.Model)
	case "/profiles":
		printProfiles(output, *state)
	case "/models":
		fmt.Fprintln(output, "Команда /models получает список из активного API; встроенный справочник: --list-models")
	case "/formats":
		printFormatCatalog(output)
	case "/settings":
		printSessionSettings(output, *state)
	case "/status":
		printRequestStatus(output, state.LastRequest)
	case "/clear":
		fmt.Fprintln(output, "Команда /clear доступна в полноэкранном TUI")
	default:
		fmt.Fprintln(output, "Неизвестная команда. Используйте /help")
	}
}

func reloadSessionConfig(currentToken string, state *sessionState) (string, string, error) {
	configPath, err := defaultConfigPath()
	if err != nil {
		return currentToken, "", err
	}
	config, err := loadConfigForReload(configPath, currentToken, state.API.BaseURL)
	if err != nil {
		return currentToken, configPath, fmt.Errorf("прочитать %s: %w", configPath, err)
	}

	mode := modeControlled
	if !config.ResponseControl.Enabled {
		mode = modeFree
	}
	state.Mode = mode
	profile, _ := config.activeAPIProfile()
	state.ActiveProfile = config.ActiveProfile
	state.Profiles = config.Profiles
	state.Model = profile.Model
	state.API = profile
	state.APIToken = config.APIToken
	state.Temperature = config.Generation.Temperature
	state.Strategy = config.Generation.Strategy
	state.Control = config.ResponseControl
	return config.APIToken, configPath, nil
}

func printSessionSettings(output io.Writer, state sessionState) {
	fmt.Fprintf(output, "Активный профиль: %s\n", state.ActiveProfile)
	fmt.Fprintf(output, "Модель: %s\n", state.Model)
	fmt.Fprintf(output, "Режим: %s\n", state.Mode)
	fmt.Fprintf(output, "Стратегия: %s\n", state.Strategy)
	fmt.Fprintf(output, "Temperature: %g\n", state.Temperature)
	controlStatus := "выключены"
	if state.Control.Enabled {
		controlStatus = "включены"
	}
	fmt.Fprintf(output, "Ограничения ответа: %s\n", controlStatus)
	fmt.Fprintf(output, "Формат: %s\n", state.Control.Format)
	fmt.Fprintf(output, "Максимум слов: %d\n", state.Control.MaxWords)
	fmt.Fprintf(output, "Максимум токенов: %d\n", state.Control.MaxTokens)
	fmt.Fprintf(output, "Stop sequences: %q\n", state.Control.StopSequences)
}

type temperatureBenchmarkResult struct {
	Temperature float64
	Answer      completionResult
}

func printRequestStatus(output io.Writer, status *requestStatus) {
	if status == nil {
		fmt.Fprintln(output, "Статус пока недоступен: сначала отправьте запрос модели.")
		return
	}

	result := status.Result
	cached := min(result.CachedInputTokens, result.PromptTokens)
	fmt.Fprintln(output, "Последний API-запрос")
	fmt.Fprintf(output, "Профиль: %s\n", status.Profile)
	fmt.Fprintf(output, "Модель: %s\n", resultModel(result))
	fmt.Fprintf(output, "Токены: вход=%d (кэш=%d), выход=%d, всего=%d\n",
		result.PromptTokens, cached, result.CompletionTokens, result.TotalTokens)
	fmt.Fprintf(output, "Скорость: %.1f token/sec\n", tokensPerSecond(result))
	fmt.Fprintf(output, "Время: %.2f с\n", result.Duration.Seconds())

	cost, known := requestCost(*status)
	if !known {
		fmt.Fprintln(output, "Стоимость: неизвестна для этого API или модели")
		return
	}
	fmt.Fprintf(output, "Оценка стоимости: $%.8f USD\n", cost)
}

func printTemperatureBenchmark(output io.Writer, results []temperatureBenchmarkResult) {
	for _, result := range results {
		fmt.Fprintf(output, "\nTEMPERATURE = %g\n", result.Temperature)
		fmt.Fprintln(output, strings.Repeat("-", 40))
		printSingleAnswer(output, result.Answer, nil)
	}

	fmt.Fprintln(output, "\nСВОДКА БЕНЧМАРКА")
	fmt.Fprintln(output, strings.Repeat("-", 40))
	for _, result := range results {
		answer := result.Answer
		fmt.Fprintf(output,
			"temperature=%g: %d слов, %d токенов, разнообразие=%.2f, %.1f ток/с, %.2f с, finish_reason=%s\n",
			result.Temperature,
			wordCount(answer.Content),
			answer.CompletionTokens,
			lexicalDiversity(answer.Content),
			tokensPerSecond(answer),
			answer.Duration.Seconds(),
			answer.FinishReason,
		)
	}
	fmt.Fprintln(output, "Разнообразие — доля уникальных слов от 0 до 1; точность оценивайте по содержанию ответов.")
}

func lexicalDiversity(text string) float64 {
	words := strings.Fields(strings.ToLower(text))
	if len(words) == 0 {
		return 0
	}
	unique := make(map[string]struct{}, len(words))
	for _, word := range words {
		word = strings.Trim(word, ".,!?;:()[]{}\"'«»—-")
		if word != "" {
			unique[word] = struct{}{}
		}
	}
	return float64(len(unique)) / float64(len(words))
}

func activateSessionProfile(state *sessionState, name string) error {
	profile, ok := state.Profiles[name]
	if !ok {
		return fmt.Errorf("неизвестный профиль %q; используйте /profiles", name)
	}
	token := ""
	if profile.APIKeyEnv != "" {
		token = strings.TrimSpace(os.Getenv(profile.APIKeyEnv))
		if token == "" {
			return fmt.Errorf("не найдена переменная окружения %s", profile.APIKeyEnv)
		}
	}
	state.ActiveProfile = name
	state.API = profile
	state.APIToken = token
	state.Model = profile.Model
	return nil
}

func printProfiles(output io.Writer, state sessionState) {
	names := make([]string, 0, len(state.Profiles))
	for name := range state.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		profile := state.Profiles[name]
		marker := " "
		if name == state.ActiveProfile {
			marker = "*"
		}
		fmt.Fprintf(output, "%s %-16s model=%s\n", marker, name, profile.Model)
	}
}

func printSingleAnswer(output io.Writer, answer completionResult, validation *validationResult) {
	fmt.Fprintln(output, formatAnswer(answer.Content))
	fmt.Fprintf(output, "\nМетрики: model=%s, %d слов, %d символов, %d токенов, %.1f ток/с, %.2f с, finish_reason=%s\n",
		resultModel(answer),
		wordCount(answer.Content), utf8.RuneCountInString(answer.Content),
		answer.CompletionTokens, tokensPerSecond(answer), answer.Duration.Seconds(), answer.FinishReason)
	if validation != nil {
		status := "OK"
		if !validation.Passed {
			status = "FAIL"
		}
		fmt.Fprintf(output, "Проверка ограничений: %s\n", status)
	}
}
