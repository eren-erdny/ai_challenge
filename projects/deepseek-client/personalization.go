package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

type userProfileStore interface {
	Active(context.Context) (agent.UserProfile, error)
	List(context.Context) ([]agent.UserProfile, error)
	Get(context.Context, string) (agent.UserProfile, error)
	Create(context.Context, string) (agent.UserProfile, error)
	Save(context.Context, agent.UserProfile) error
	Select(context.Context, string) (agent.UserProfile, error)
	Delete(context.Context, string) error
}

func personaWizardPrompt(stage int) string {
	switch stage {
	case 1:
		return "Укажите стиль ответа (Enter или - — пропустить, /cancel — завершить):"
	case 2:
		return "Укажите формат ответа (Enter или - — пропустить):"
	case 3:
		return "Укажите ограничения через ; (Enter или - — пропустить):"
	default:
		return ""
	}
}

func applyPersonaWizardAnswer(ctx context.Context, state *sessionState, stage int, answer string) (int, error) {
	if state.UserProfile.Name == "" || state.UserProfiles == nil {
		return 0, errors.New("active user profile is required")
	}
	answer = strings.TrimSpace(answer)
	if answer == "/cancel" {
		return -1, nil
	}
	if answer == "-" {
		answer = ""
	}
	old := state.UserProfile
	switch stage {
	case 1:
		state.UserProfile.Style = answer
	case 2:
		state.UserProfile.Format = answer
	case 3:
		state.UserProfile.Constraints = nil
		for _, value := range strings.Split(answer, ";") {
			if value = strings.TrimSpace(value); value != "" {
				state.UserProfile.Constraints = append(state.UserProfile.Constraints, value)
			}
		}
	default:
		return 0, errors.New("invalid persona setup stage")
	}
	if err := state.UserProfiles.Save(ctx, state.UserProfile); err != nil {
		state.UserProfile = old
		return stage, err
	}
	if stage == 3 {
		return 0, nil
	}
	return stage + 1, nil
}

func printUserProfile(output io.Writer, profile agent.UserProfile) {
	if profile.Name == "" {
		fmt.Fprintln(output, "Профиль пользователя не выбран")
		return
	}
	fmt.Fprintf(output, "Профиль пользователя: %s\n  Стиль: %s\n  Формат: %s\n", profile.Name, profile.Style, profile.Format)
	if len(profile.Constraints) == 0 {
		fmt.Fprintln(output, "  Ограничения: нет")
	}
	for i, constraint := range profile.Constraints {
		fmt.Fprintf(output, "  Ограничение %d: %s\n", i+1, constraint)
	}
}

func handlePersonalizationCommand(ctx context.Context, command string, state *sessionState, output io.Writer) bool {
	fields := strings.Fields(command)
	if len(fields) < 2 || fields[0] != "/persona" {
		return false
	}
	if state.UserProfiles == nil {
		fmt.Fprintln(output, "Хранилище профилей пользователя недоступно")
		return true
	}
	switch fields[1] {
	case "create":
		if len(fields) != 3 {
			fmt.Fprintln(output, "Использование: /persona create NAME")
			return true
		}
		profile, err := state.UserProfiles.Create(ctx, fields[2])
		if err != nil {
			fmt.Fprintf(output, "Профиль не создан: %v\n", err)
			return true
		}
		state.UserProfile = profile
		fmt.Fprintf(output, "Создан и выбран профиль пользователя: %s\n", profile.Name)
	case "use":
		if len(fields) != 3 {
			fmt.Fprintln(output, "Использование: /persona use NAME")
			return true
		}
		profile, err := state.UserProfiles.Select(ctx, fields[2])
		if err != nil {
			fmt.Fprintf(output, "Профиль не выбран: %v\n", err)
			return true
		}
		state.UserProfile = profile
		fmt.Fprintf(output, "Выбран профиль пользователя: %s\n", profile.Name)
	case "show":
		profile := state.UserProfile
		var err error
		if len(fields) == 3 {
			profile, err = state.UserProfiles.Get(ctx, fields[2])
		} else if len(fields) != 2 {
			fmt.Fprintln(output, "Использование: /persona show [NAME]")
			return true
		}
		if err != nil {
			fmt.Fprintf(output, "Профиль не прочитан: %v\n", err)
			return true
		}
		printUserProfile(output, profile)
	case "list":
		profiles, err := state.UserProfiles.List(ctx)
		if err != nil {
			fmt.Fprintf(output, "Профили не прочитаны: %v\n", err)
			return true
		}
		for _, profile := range profiles {
			marker := " "
			if profile.Name == state.UserProfile.Name {
				marker = "*"
			}
			fmt.Fprintf(output, "%s %s\n", marker, profile.Name)
		}
	case "set":
		if len(fields) < 4 || (fields[2] != "style" && fields[2] != "format") || state.UserProfile.Name == "" {
			fmt.Fprintln(output, "Использование: /persona set style|format VALUE (нужен активный профиль)")
			return true
		}
		old := state.UserProfile
		value := strings.Join(fields[3:], " ")
		if fields[2] == "style" {
			state.UserProfile.Style = value
		} else {
			state.UserProfile.Format = value
		}
		if err := state.UserProfiles.Save(ctx, state.UserProfile); err != nil {
			state.UserProfile = old
			fmt.Fprintf(output, "Профиль не сохранён: %v\n", err)
			return true
		}
		fmt.Fprintf(output, "Обновлено поле %s профиля %s\n", fields[2], state.UserProfile.Name)
	case "add-constraint":
		if len(fields) < 3 || state.UserProfile.Name == "" {
			fmt.Fprintln(output, "Использование: /persona add-constraint VALUE (нужен активный профиль)")
			return true
		}
		state.UserProfile.Constraints = append(state.UserProfile.Constraints, strings.Join(fields[2:], " "))
		if err := state.UserProfiles.Save(ctx, state.UserProfile); err != nil {
			state.UserProfile.Constraints = state.UserProfile.Constraints[:len(state.UserProfile.Constraints)-1]
			fmt.Fprintf(output, "Ограничение не сохранено: %v\n", err)
			return true
		}
		fmt.Fprintln(output, "Ограничение добавлено")
	case "remove-constraint":
		if len(fields) != 3 || state.UserProfile.Name == "" {
			fmt.Fprintln(output, "Использование: /persona remove-constraint N")
			return true
		}
		index, err := strconv.Atoi(fields[2])
		if err != nil || index < 1 || index > len(state.UserProfile.Constraints) {
			fmt.Fprintln(output, "Номер ограничения вне диапазона")
			return true
		}
		old := append([]string(nil), state.UserProfile.Constraints...)
		state.UserProfile.Constraints = append(state.UserProfile.Constraints[:index-1], state.UserProfile.Constraints[index:]...)
		if err := state.UserProfiles.Save(ctx, state.UserProfile); err != nil {
			state.UserProfile.Constraints = old
			fmt.Fprintf(output, "Ограничение не удалено: %v\n", err)
			return true
		}
		fmt.Fprintln(output, "Ограничение удалено")
	case "delete":
		if len(fields) != 3 {
			fmt.Fprintln(output, "Использование: /persona delete NAME")
			return true
		}
		if err := state.UserProfiles.Delete(ctx, fields[2]); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				fmt.Fprintln(output, "Профиль не найден")
			} else {
				fmt.Fprintf(output, "Профиль не удалён: %v\n", err)
			}
			return true
		}
		if state.UserProfile.Name == fields[2] {
			state.UserProfile = agent.UserProfile{}
		}
		fmt.Fprintf(output, "Профиль удалён: %s\n", fields[2])
	default:
		fmt.Fprintln(output, "Использование: /persona create|use|show|list|set|add-constraint|remove-constraint|delete")
	}
	return true
}
