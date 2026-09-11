package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/cursor"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"golang.org/x/term"
)

type answerMessage struct {
	text       string
	lastStatus *requestStatus
}

type modelListMessage struct {
	models []string
	err    error
}

type activityTickMessage struct {
	generation int
}

type autocompleteSuggestion struct {
	value string
}

const (
	historyViewportHeight = 32
	panelFrameWidth       = 4
)

type tuiModel struct {
	ctx             context.Context
	cancel          context.CancelFunc
	textarea        textarea.Model
	viewport        viewport.Model
	state           sessionState
	ask             askFunction
	history         []string
	busy            bool
	width           int
	height          int
	suggestionIndex int
	availableModels []string
	activityFrame   int
	activityGen     int
}

var (
	titleStyle              = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	userStyle               = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	statusStyle             = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errorStyle              = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	modelStyle              = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	modeStyle               = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	strategyStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("207"))
	temperatureStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	formatStyle             = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	readyStyle              = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
	busyStyle               = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	suggestionStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	selectedSuggestionStyle = lipgloss.NewStyle().Bold(true).Reverse(true).Foreground(lipgloss.Color("230"))
	panelStyle              = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
)

var commandSuggestions = []autocompleteSuggestion{
	{value: "/tools"},
	{value: "/context "},
	{value: "/new"},
	{value: "/conversation"},
	{value: "/model "},
	{value: "/models"},
	{value: "/profile "},
	{value: "/profiles"},
	{value: "/mode "},
	{value: "/strategy "},
	{value: "/temperature "},
	{value: "/format "},
	{value: "/formats"},
	{value: "/settings"},
	{value: "/status"},
	{value: "/reload"},
	{value: "/clear"},
	{value: "/help"},
	{value: "/exit"},
}

func isInteractiveTerminal(input *os.File, output *os.File) bool {
	return term.IsTerminal(int(input.Fd())) && term.IsTerminal(int(output.Fd()))
}

func runTUI(config appConfig, ask askFunction) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newTUIModel(config, ask)
	model.ctx, model.cancel = ctx, cancel
	program := tea.NewProgram(model)
	final, err := program.Run()
	if closed, ok := final.(tuiModel); ok {
		printConversationExit(os.Stdout, closed.state)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "ошибка TUI: %v\n", err)
		return 1
	}
	return 0
}

func newTUIModel(config appConfig, ask askFunction) tuiModel {
	mode := modeControlled
	if !config.ResponseControl.Enabled {
		mode = modeFree
	}
	profile, _ := config.activeAPIProfile()
	state := sessionState{
		Tools: config.Tools, DocumentsDirectory: config.DocumentsDirectory,
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

	input := textarea.New()
	input.Placeholder = "Введите запрос или /help"
	input.Prompt = "┃ "
	input.CharLimit = 50_000
	input.SetWidth(80)
	input.SetHeight(3)
	input.ShowLineNumbers = false
	input.KeyMap.InsertNewline.SetKeys("ctrl+j", "alt+enter")
	input.Focus()

	history := []string{
		titleStyle.Render("DeepSeek Client"),
		statusStyle.Render("Conversation ID: " + state.ConversationID),
		"Вставьте многострочный запрос и нажмите Enter. Команда /help покажет настройки.",
	}
	view := viewport.New(viewport.WithWidth(80), viewport.WithHeight(16))
	if config.HistoryNotice != "" {
		history = append(history, statusStyle.Render(config.HistoryNotice))
	}
	for _, message := range config.InitialMessages {
		label := userStyle.Render("Вы")
		if message.Role == "assistant" {
			label = titleStyle.Render("Ассистент")
		}
		history = append(history, label+"\n"+message.Content)
	}
	view.SetContent(strings.Join(history, "\n\n"))
	view.GotoBottom()

	return tuiModel{
		ctx:      context.Background(),
		textarea: input,
		viewport: view,
		state:    state,
		ask:      ask,
		history:  history,
	}
}

func (model tuiModel) Init() tea.Cmd {
	return textarea.Blink
}

func (model tuiModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.resize(message.Width, message.Height)
		return model, nil
	case answerMessage:
		model.busy = false
		model.activityFrame = 0
		if message.lastStatus != nil {
			model.state.LastRequest = message.lastStatus
		}
		model.history = append(model.history, titleStyle.Render("DeepSeek")+"\n"+message.text)
		model.refreshHistory()
		return model, nil
	case modelListMessage:
		model.busy = false
		model.activityFrame = 0
		if message.err != nil {
			model.history = append(model.history, errorStyle.Render("Не удалось получить модели: "+message.err.Error()))
		} else {
			model.availableModels = message.models
			model.history = append(model.history, statusStyle.Render("Модели активного API:\n"+strings.Join(message.models, "\n")))
		}
		model.refreshHistory()
		return model, nil
	case activityTickMessage:
		if !model.busy || message.generation != model.activityGen {
			return model, nil
		}
		model.activityFrame = (model.activityFrame + 1) % 4
		return model, activityTickCommand(model.activityGen)
	case tea.MouseWheelMsg:
		mouse := message.Mouse()
		if model.mouseInsideDialog(mouse.X, mouse.Y) {
			var command tea.Cmd
			model.viewport, command = model.viewport.Update(message)
			return model, command
		}
		return model, nil
	case tea.KeyPressMsg:
		switch message.String() {
		case "ctrl+c", "esc":
			if model.cancel != nil {
				model.cancel()
			}
			return model, tea.Quit
		case "up", "down":
			suggestions := model.autocompleteSuggestions()
			if len(suggestions) > 0 {
				if message.String() == "up" {
					model.suggestionIndex = (model.suggestionIndex - 1 + len(suggestions)) % len(suggestions)
				} else {
					model.suggestionIndex = (model.suggestionIndex + 1) % len(suggestions)
				}
				return model, nil
			}
		case "tab":
			if model.applySelectedSuggestion() {
				return model, nil
			}
		case "enter":
			if model.busy {
				return model, nil
			}
			if model.applySelectedSuggestion() {
				return model, nil
			}
			return model.submit()
		case "pgup", "pgdown", "home", "end":
			var command tea.Cmd
			model.viewport, command = model.viewport.Update(message)
			return model, command
		}
	case cursor.BlinkMsg:
		var command tea.Cmd
		model.textarea, command = model.textarea.Update(message)
		return model, command
	}

	var inputCommand tea.Cmd
	previousValue := model.textarea.Value()
	model.textarea, inputCommand = model.textarea.Update(message)
	if model.textarea.Value() != previousValue {
		model.suggestionIndex = 0
	}
	var viewportCommand tea.Cmd
	model.viewport, viewportCommand = model.viewport.Update(message)
	return model, tea.Batch(inputCommand, viewportCommand)
}

func (model tuiModel) submit() (tea.Model, tea.Cmd) {
	text := normalizePastedText(model.textarea.Value())
	if text == "" {
		return model, nil
	}
	model.textarea.Reset()
	model.suggestionIndex = 0

	if text == "/exit" || strings.EqualFold(text, "exit") || strings.EqualFold(text, "выход") {
		return model, tea.Quit
	}
	var conversationOutput strings.Builder
	if handled, changed, messages := handleConversationCommand(model.ctx, text, &model.state, &conversationOutput); handled {
		if changed {
			model.history = nil
			for _, message := range messages {
				label := userStyle.Render("Вы")
				if message.Role == "assistant" {
					label = titleStyle.Render("Ассистент")
				}
				model.history = append(model.history, label+"\n"+message.Content)
			}
		}
		model.history = append(model.history, statusStyle.Render(conversationOutput.String()))
		model.refreshHistory()
		return model, nil
	}
	if text == "/clear" {
		model.history = nil
		model.refreshHistory()
		return model, nil
	}
	if text == "/reload" {
		newToken, configPath, err := reloadSessionConfig(model.state.APIToken, &model.state)
		if err != nil {
			model.history = append(model.history, errorStyle.Render("Не удалось перезагрузить конфигурацию: "+err.Error()))
		} else {
			model.state.APIToken = newToken
			model.history = append(model.history, statusStyle.Render("Конфигурация перезагружена: "+configPath))
		}
		model.refreshHistory()
		return model, nil
	}
	if text == "/models" {
		model.busy = true
		model.activityFrame = 0
		model.activityGen++
		model.history = append(model.history, statusStyle.Render("Получаю список моделей..."))
		model.refreshHistory()
		return model, tea.Batch(fetchModelsCommand(model.ctx, model.state.APIToken, model.state.API), activityTickCommand(model.activityGen))
	}
	if strings.HasPrefix(text, "/") {
		var output strings.Builder
		previousProfile := model.state.ActiveProfile
		handleSessionCommand(text, &model.state, &output)
		if model.state.ActiveProfile != previousProfile {
			model.availableModels = nil
		}
		model.history = append(model.history, statusStyle.Render(output.String()))
		model.refreshHistory()
		return model, nil
	}

	model.history = append(model.history, userStyle.Render("Вы")+"\n"+text)
	model.busy = true
	model.activityFrame = 0
	model.activityGen++
	model.refreshHistory()
	return model, tea.Batch(
		executeQuestionCommand(model.ctx, model.state.APIToken, text, model.state, model.ask),
		activityTickCommand(model.activityGen),
	)
}

func activityTickCommand(generation int) tea.Cmd {
	return tea.Tick(350*time.Millisecond, func(time.Time) tea.Msg {
		return activityTickMessage{generation: generation}
	})
}

func fetchModelsCommand(ctx context.Context, token string, profile apiProfile) tea.Cmd {
	return func() tea.Msg {
		models, err := fetchModels(ctx, token, profile)
		return modelListMessage{models: models, err: err}
	}
}

func executeQuestionCommand(ctx context.Context, token string, prompt string, state sessionState, ask askFunction) tea.Cmd {
	return func() tea.Msg {
		var output strings.Builder
		var errorOutput strings.Builder
		lastStatus := executeQuestion(ctx, token, prompt, state, &output, &errorOutput, ask)
		text := strings.TrimSpace(output.String())
		if errorOutput.Len() > 0 {
			errorText := errorStyle.Render(strings.TrimSpace(errorOutput.String()))
			if text == "" {
				text = errorText
			} else {
				text += "\n\n" + errorText
			}
		}
		return answerMessage{text: text, lastStatus: lastStatus}
	}
}

func (model *tuiModel) resize(width, height int) {
	model.width = width
	model.height = height
	contentWidth := max(30, width-panelFrameWidth)
	model.textarea.SetWidth(contentWidth)
	model.viewport.SetWidth(contentWidth)
	availableHeight := max(4, height-model.textarea.Height()-12)
	model.viewport.SetHeight(min(historyViewportHeight, availableHeight))
	model.refreshHistory()
}

func (model *tuiModel) refreshHistory() {
	width := max(30, model.viewport.Width())
	content := lipgloss.NewStyle().Width(width).Render(strings.Join(model.history, "\n\n"))
	model.viewport.SetContent(content)
	model.viewport.GotoBottom()
}

func (model tuiModel) View() tea.View {
	activity := model.activityText()
	help := model.helpLine()
	dialogContent := titleStyle.Render("Диалог") + "\n" + model.viewport.View()
	dialogPanel := panelStyle.Width(model.viewport.Width()).Render(dialogContent)
	composerContent := titleStyle.Render("Ввод и настройки") + "\n" +
		model.textarea.View() + "\n" + model.compactSettings(activity)
	composerPanel := panelStyle.Width(model.viewport.Width()).Render(composerContent)
	body := dialogPanel + "\n" + composerPanel + "\n" + statusStyle.Render(help)

	view := tea.NewView(body)
	cursorPosition := model.textarea.Cursor()
	if cursorPosition != nil {
		cursorPosition.X += 2
		cursorPosition.Y += lipgloss.Height(dialogPanel) + 2
	}
	view.Cursor = cursorPosition
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func (model *tuiModel) applySelectedSuggestion() bool {
	suggestions := model.autocompleteSuggestions()
	if len(suggestions) == 0 {
		return false
	}
	index := model.suggestionIndex % len(suggestions)
	value := suggestions[index].value
	if model.textarea.Value() == value {
		return false
	}
	model.textarea.SetValue(value)
	model.textarea.CursorEnd()
	model.suggestionIndex = 0
	return true
}

func (model tuiModel) autocompleteSuggestions() []autocompleteSuggestion {
	value := model.textarea.Value()
	if !strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\r\n") {
		return nil
	}

	space := strings.IndexByte(value, ' ')
	if space < 0 {
		return matchingSuggestions(commandSuggestions, value)
	}

	command := value[:space]
	argument := strings.TrimSpace(value[space+1:])
	if strings.Contains(argument, " ") {
		return nil
	}

	var values []string
	switch command {
	case "/model":
		if len(model.availableModels) > 0 {
			values = append(values, model.availableModels...)
		} else {
			for _, definition := range modelCatalog {
				values = append(values, definition.Name)
			}
		}
	case "/mode":
		values = []string{string(modeFree), string(modeControlled), string(modeCompare), string(modeTemperatureBenchmark), string(modeModelBenchmark)}
	case "/strategy":
		values = []string{string(strategyStandard), string(strategyStepByStep), string(strategyExperts)}
	case "/temperature":
		values = []string{"0", "0.7", "1.2"}
	case "/profile":
		for name := range model.state.Profiles {
			values = append(values, name)
		}
		sort.Strings(values)
	case "/format":
		for _, definition := range formatCatalog {
			values = append(values, definition.Name)
		}
	default:
		return nil
	}

	suggestions := make([]autocompleteSuggestion, 0, len(values))
	for _, candidate := range values {
		if strings.HasPrefix(strings.ToLower(candidate), strings.ToLower(argument)) {
			suggestions = append(suggestions, autocompleteSuggestion{value: command + " " + candidate})
		}
	}
	return suggestions
}

func matchingSuggestions(candidates []autocompleteSuggestion, prefix string) []autocompleteSuggestion {
	matches := make([]autocompleteSuggestion, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.HasPrefix(strings.ToLower(candidate.value), strings.ToLower(prefix)) {
			matches = append(matches, candidate)
		}
	}
	return matches
}

func (model tuiModel) helpLine() string {
	suggestions := model.autocompleteSuggestions()
	if len(suggestions) == 0 {
		return "Enter отправить  •  Ctrl+J перенос  •  Колесо/PgUp/PgDn история  •  Esc выход"
	}

	index := model.suggestionIndex % len(suggestions)
	start := max(0, index-1)
	end := min(len(suggestions), start+3)
	if end-start < 3 {
		start = max(0, end-3)
	}
	items := make([]string, 0, end-start)
	for suggestionIndex := start; suggestionIndex < end; suggestionIndex++ {
		value := suggestions[suggestionIndex].value
		if suggestionIndex == index {
			items = append(items, selectedSuggestionStyle.Render(" "+value+" "))
		} else {
			items = append(items, suggestionStyle.Render(value))
		}
	}
	return "↑/↓ выбрать  " + strings.Join(items, "  ") + "  •  Tab/Enter подставить"
}

func (model tuiModel) mouseInsideDialog(x, y int) bool {
	const viewportTop = 2
	return x >= 0 && x < model.viewport.Width()+panelFrameWidth &&
		y >= viewportTop && y < viewportTop+model.viewport.Height()
}

func (model tuiModel) activityText() string {
	activity := "готов"
	if model.busy {
		activity = fmt.Sprintf("обрабатываю запрос%-3s", strings.Repeat(".", model.activityFrame))
	}
	return activity
}

func (model tuiModel) compactSettings(activity string) string {
	activityStyle := readyStyle
	if model.busy {
		activityStyle = busyStyle
	}
	format := model.state.Control.Format
	if !model.state.Control.Enabled {
		format = "off"
	}
	firstLine := modeStyle.Render(fmt.Sprintf("profile=%s", model.state.ActiveProfile))
	secondLine := strings.Join([]string{
		modelStyle.Render(fmt.Sprintf("model=%s", model.state.Model)),
		modeStyle.Render(fmt.Sprintf("mode=%s", model.state.Mode)),
		strategyStyle.Render(fmt.Sprintf("strategy=%s", model.state.Strategy)),
	}, "  |  ")
	thirdLine := strings.Join([]string{
		temperatureStyle.Render(fmt.Sprintf("temperature=%g", model.state.Temperature)),
		formatStyle.Render(fmt.Sprintf("format=%s", format)),
		activityStyle.Render(activity),
	}, "  |  ")
	return firstLine + "\n" + secondLine + "\n" + thirdLine
}

func normalizePastedText(value string) string {
	replacer := strings.NewReplacer(
		"\u00a0", " ",
		"&nbsp;", " ",
		"&#160;", " ",
		"&#xA0;", " ",
		"&#xa0;", " ",
	)
	return strings.TrimSpace(replacer.Replace(value))
}
