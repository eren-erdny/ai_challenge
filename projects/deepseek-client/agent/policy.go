package agent

import (
	"errors"
	"fmt"
	"strings"
)

type FormatDefinition struct {
	Name        string
	Description string
	Instruction string
}

var formatCatalog = []FormatDefinition{
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

func ValidateControlConfig(config ControlConfig) error {
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

	definition, ok := FindFormat(config.Format)
	if !ok {
		return fmt.Errorf("неизвестный формат %q; используйте --list-formats", config.Format)
	}
	if definition.Name == "custom" && strings.TrimSpace(config.CustomInstruction) == "" {
		return errors.New("для формата custom заполните custom_instruction")
	}

	return nil
}
func FindFormat(name string) (FormatDefinition, bool) {
	for _, definition := range formatCatalog {
		if definition.Name == name {
			return definition, true
		}
	}
	return FormatDefinition{}, false
}
func BuildControl(config ControlConfig) (*Control, error) {
	if !config.Enabled {
		return nil, nil
	}
	if err := ValidateControlConfig(config); err != nil {
		return nil, err
	}

	definition, _ := FindFormat(config.Format)
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

	return &Control{
		Format:       config.Format,
		SystemPrompt: systemPrompt,
		MaxWords:     config.MaxWords,
		MaxTokens:    config.MaxTokens,
		Stop:         append([]string(nil), config.StopSequences...),
	}, nil
}
func StrategyInstruction(strategy Strategy) string {
	switch strategy {
	case StepByStep:
		return "Решай задачу пошагово: сначала выдели исходные данные, затем покажи ход решения и проверь итог."
	case Experts:
		return `Рассмотри запрос как группа из трёх экспертов:
1. Аналитик формализует задачу и предлагает решение.
2. Инженер предлагает практический или алгоритмический подход.
3. Критик проверяет решения, указывает ошибки и ограничения.
После отдельных мнений сформулируй общий итог.`
	default:
		return ""
	}
}

func Formats() []FormatDefinition { return append([]FormatDefinition(nil), formatCatalog...) }

// PrepareMessages is the input policy shared by every model invocation.
func PrepareMessages(prompt string, settings Settings) []Message {
	var instructions []string
	if instruction := StrategyInstruction(settings.Strategy); instruction != "" {
		instructions = append(instructions, instruction)
	}
	if settings.Control != nil {
		instructions = append(instructions, settings.Control.SystemPrompt)
	}
	messages := make([]Message, 0, 2)
	if len(instructions) > 0 {
		messages = append(messages, Message{Role: "system", Content: strings.Join(instructions, "\n\n")})
	}
	return append(messages, Message{Role: "user", Content: prompt})
}
