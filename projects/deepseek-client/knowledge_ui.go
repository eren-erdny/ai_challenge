package main

import (
	"bufio"
	tea "charm.land/bubbletea/v2"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
)

type knowledgeMessage struct{ text string }

func refreshKnowledgeLabel(state *sessionState) {
	state.KnowledgeLabel = "выключена"
	if state.Knowledge != nil {
		base, err := state.Knowledge.Selected(conversationID(state.ConversationID))
		if err != nil {
			state.KnowledgeLabel = "ошибка каталога"
		} else if base.ID != "" {
			state.KnowledgeLabel = base.Name
		}
	}
}
func knowledgeMenu(state sessionState) string {
	return fmt.Sprintf("Базы знаний · этот чат: %s\n1 — Создать базу\n2 — Выбрать базу для чата\n3 — Добавить файл или папку\n4 — Обновить индекс\n5 — Убрать источник из базы\n6 — Отключить базу в этом чате\n0 — Закрыть меню\nПри первом создании загрузятся локальные модели. Фрагменты для ответа отправляются выбранному LLM-провайдеру.", state.KnowledgeLabel)
}
func knowledgeChoices(state sessionState) (string, error) {
	bases, err := state.Knowledge.List()
	if err != nil {
		return "", err
	}
	if len(bases) == 0 {
		return "Баз пока нет. Закройте меню и выберите «Создать базу».", nil
	}
	var out strings.Builder
	for i, b := range bases {
		fmt.Fprintf(&out, "%d — %s (%d источников)\n", i+1, b.Name, len(b.Sources))
	}
	fmt.Fprintln(&out, "Введите номер базы (0 — закрыть).")
	return out.String(), nil
}
func chooseKnowledge(state *sessionState, number string) error {
	bases, err := state.Knowledge.List()
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(number)
	if err != nil || n < 1 || n > len(bases) {
		return fmt.Errorf("введите номер базы из списка")
	}
	if err = state.Knowledge.Select(conversationID(state.ConversationID), bases[n-1].ID); err == nil {
		refreshKnowledgeLabel(state)
	}
	return err
}
func knowledgeJob(ctx context.Context, state sessionState, action, name, path string) string {
	var log strings.Builder
	var err error
	if action == "create" {
		_, err = state.Knowledge.Create(ctx, name, path, conversationID(state.ConversationID), &log)
	} else {
		base, selectedErr := state.Knowledge.Selected(conversationID(state.ConversationID))
		err = selectedErr
		if err == nil && base.ID == "" {
			err = fmt.Errorf("сначала выберите базу")
		}
		if err == nil {
			switch action {
			case "add":
				err = state.Knowledge.Update(ctx, base.ID, path, &log)
			case "update":
				err = state.Knowledge.Update(ctx, base.ID, "", &log)
			case "remove":
				n, parseErr := strconv.Atoi(path)
				if parseErr != nil {
					err = fmt.Errorf("введите номер источника")
				} else {
					err = state.Knowledge.RemoveSource(ctx, base.ID, n, &log)
				}
			}
		}
	}
	if err != nil {
		return "База не обновлена: " + err.Error() + "\n" + toolPreview(log.String(), state.APIToken)
	}
	return "База знаний готова. Задавайте вопросы обычным сообщением; поиск по выбранной базе выполняется автоматически."
}
func (model tuiModel) openKnowledge() (tea.Model, tea.Cmd) {
	if model.state.Knowledge == nil {
		model.history = append(model.history, "Управление базами знаний недоступно.")
		model.refreshHistory()
		return model, nil
	}
	refreshKnowledgeLabel(&model.state)
	model.knowledgeStage = "menu"
	model.history = append(model.history, knowledgeMenu(model.state))
	model.refreshHistory()
	return model, nil
}
func (model tuiModel) submitKnowledge(text string) (tea.Model, tea.Cmd) {
	text = strings.TrimSpace(text)
	if text == "/cancel" || text == "0" {
		model.knowledgeStage = ""
		model.history = append(model.history, "Меню баз знаний закрыто.")
		model.refreshHistory()
		return model, nil
	}
	var message string
	switch model.knowledgeStage {
	case "menu":
		switch text {
		case "1":
			model.knowledgeStage = "name"
			message = "Название новой базы знаний (например, Работа). /cancel — отмена."
		case "2":
			model.knowledgeStage = "choose"
			var err error
			message, err = knowledgeChoices(model.state)
			if err != nil {
				message = err.Error()
			}
		case "3":
			model.knowledgeStage = "path"
			model.knowledgeAction = "add"
			message = "Путь к файлу или папке для добавления. /cancel — отмена."
		case "4":
			return model.startKnowledgeJob("update", "")
		case "5":
			base, err := model.state.Knowledge.Selected(conversationID(model.state.ConversationID))
			if err != nil || base.ID == "" {
				message = "Сначала выберите базу."
			} else {
				model.knowledgeStage = "remove"
				var out strings.Builder
				for i, path := range base.Sources {
					fmt.Fprintf(&out, "%d — %s\n", i+1, path)
				}
				message = out.String() + "Введите номер источника для удаления из индекса. Исходные файлы останутся. /cancel — отмена."
			}
		case "6":
			if err := model.state.Knowledge.Select(conversationID(model.state.ConversationID), ""); err != nil {
				message = err.Error()
			} else {
				refreshKnowledgeLabel(&model.state)
				model.knowledgeStage = ""
				message = "Поиск по базе в этом чате отключён."
			}
		default:
			message = "Введите номер действия от 0 до 6."
		}
	case "name":
		if text == "" {
			message = "Введите название базы."
		} else {
			model.knowledgeName = text
			model.knowledgeAction = "create"
			model.knowledgeStage = "path"
			message = "Путь к папке документов или одному файлу. Можно вставить путь в кавычках. /cancel — отмена."
		}
	case "choose":
		if err := chooseKnowledge(&model.state, text); err != nil {
			message = err.Error()
		} else {
			model.knowledgeStage = ""
			message = "Для этого чата выбрана база «" + model.state.KnowledgeLabel + "». Задавайте вопросы обычным сообщением."
		}
	case "path":
		return model.startKnowledgeJob(model.knowledgeAction, strings.Trim(text, "\""))
	case "remove":
		return model.startKnowledgeJob("remove", text)
	}
	model.history = append(model.history, message)
	model.refreshHistory()
	return model, nil
}
func (model tuiModel) startKnowledgeJob(action, path string) (tea.Model, tea.Cmd) {
	model.knowledgeStage = ""
	model.busy = true
	model.knowledgeBusy = true
	model.activityGen++
	model.history = append(model.history, "Подготавливаю базу знаний… Первый запуск скачивает зависимости и модели и может занять несколько минут. Ctrl+C — выйти и отменить.")
	model.refreshHistory()
	state, name, ctx := model.state, model.knowledgeName, model.ctx
	return model, tea.Batch(func() tea.Msg { return knowledgeMessage{text: knowledgeJob(ctx, state, action, name, path)} }, activityTickCommand(model.activityGen))
}
func runKnowledgeMenu(input *bufio.Reader, state *sessionState, out io.Writer) {
	if state.Knowledge == nil {
		fmt.Fprintln(out, "Базы знаний недоступны.")
		return
	}
	refreshKnowledgeLabel(state)
	fmt.Fprintln(out, knowledgeMenu(*state))
	fmt.Fprint(out, "Действие: ")
	read := func() (string, error) {
		value, err := input.ReadString('\n')
		return strings.Trim(strings.TrimSpace(value), "\""), err
	}
	action, err := read()
	if err != nil {
		return
	}
	if action == "0" {
		return
	}
	if action == "6" {
		if err = state.Knowledge.Select(conversationID(state.ConversationID), ""); err != nil {
			fmt.Fprintln(out, err)
		}
		refreshKnowledgeLabel(state)
		return
	}
	if action == "2" {
		choices, err := knowledgeChoices(*state)
		if err != nil {
			fmt.Fprintln(out, err)
			return
		}
		fmt.Fprintln(out, choices)
		choice, err := read()
		if err == nil && choice != "0" {
			if err = chooseKnowledge(state, choice); err != nil {
				fmt.Fprintln(out, err)
			}
		}
		return
	}
	var name, path, operation string
	switch action {
	case "1":
		operation = "create"
		fmt.Fprint(out, "Название: ")
		name, err = read()
		if err != nil {
			return
		}
		fmt.Fprint(out, "Путь к файлу или папке: ")
		path, err = read()
	case "3":
		operation = "add"
		fmt.Fprint(out, "Путь к файлу или папке: ")
		path, err = read()
	case "4":
		operation = "update"
	case "5":
		operation = "remove"
		base, selectedErr := state.Knowledge.Selected(conversationID(state.ConversationID))
		if selectedErr != nil {
			fmt.Fprintln(out, selectedErr)
			return
		}
		for i, source := range base.Sources {
			fmt.Fprintf(out, "%d — %s\n", i+1, source)
		}
		fmt.Fprint(out, "Номер источника: ")
		path, err = read()
	default:
		fmt.Fprintln(out, "Неизвестное действие.")
		return
	}
	if err != nil || name == "/cancel" || path == "/cancel" {
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	fmt.Fprintln(out, "Подготовка базы… Первый запуск может занять несколько минут.")
	fmt.Fprintln(out, knowledgeJob(ctx, *state, operation, name, path))
	refreshKnowledgeLabel(state)
}
