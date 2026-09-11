package main

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/agent"
)

type modelBenchmarkTarget struct {
	ProfileName string
	Profile     apiProfile
	Model       string
	Token       string
}

func buildModelBenchmarkTargets(state sessionState, activeToken string) ([]modelBenchmarkTarget, error) {
	deepSeekName, deepSeekProfile, ok := findProfile(state.Profiles, isDeepSeekEndpoint)
	if !ok {
		return nil, errors.New("нужен профиль DeepSeek с base_url api.deepseek.com")
	}
	deepSeekToken, err := tokenForBenchmarkProfile(state, activeToken, deepSeekName, deepSeekProfile)
	if err != nil {
		return nil, err
	}

	ollamaName, ollamaProfile, ok := findProfile(state.Profiles, isOllamaCloudEndpoint)
	if !ok {
		return nil, errors.New("нужен профиль Ollama Cloud с base_url https://ollama.com/v1")
	}
	ollamaToken, err := tokenForBenchmarkProfile(state, activeToken, ollamaName, ollamaProfile)
	if err != nil {
		return nil, err
	}

	return []modelBenchmarkTarget{
		{ProfileName: deepSeekName, Profile: deepSeekProfile, Model: "deepseek-v4-flash", Token: deepSeekToken},
		{ProfileName: deepSeekName, Profile: deepSeekProfile, Model: "deepseek-v4-pro", Token: deepSeekToken},
		{ProfileName: ollamaName, Profile: ollamaProfile, Model: ollamaProfile.Model, Token: ollamaToken},
	}, nil
}

func findProfile(profiles map[string]apiProfile, matches func(string) bool) (string, apiProfile, bool) {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		profile := profiles[name]
		if matches(profile.BaseURL) {
			return name, profile, true
		}
	}
	return "", apiProfile{}, false
}

func tokenForBenchmarkProfile(state sessionState, activeToken string, name string, profile apiProfile) (string, error) {
	if name == state.ActiveProfile && strings.TrimSpace(activeToken) != "" {
		return activeToken, nil
	}
	if profile.APIKeyEnv == "" {
		return "", nil
	}
	token := strings.TrimSpace(os.Getenv(profile.APIKeyEnv))
	if token == "" {
		return "", fmt.Errorf("для профиля %s задайте переменную окружения %s", name, profile.APIKeyEnv)
	}
	return token, nil
}

func isOllamaCloudEndpoint(baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	return err == nil && strings.EqualFold(parsed.Hostname(), "ollama.com")
}

func printModelBenchmark(output io.Writer, results []agent.Response) {
	if len(results) == 0 {
		return
	}
	for _, result := range results {
		fmt.Fprintf(output, "\n%s:\n", result.Target.Model)
		fmt.Fprintln(output, strings.Repeat("-", 40))
		printAgentResponse(output, result)
	}
	fmt.Fprintln(output, "\nСВОДКА ПО МОДЕЛЯМ")
	fmt.Fprintln(output, strings.Repeat("-", 40))
	for _, result := range results {
		costText := "неизвестно"
		if result.CostUSD != nil {
			costText = fmt.Sprintf("$%.8f", *result.CostUSD)
		}
		controlText := ""
		if result.Validation != nil {
			status := "OK"
			if !result.Validation.Passed {
				status = "FAIL"
			}
			controlText = ", ограничения=" + status
		}
		fmt.Fprintf(output, "%s: %d токенов, %.1f token/sec, %.2f с, стоимость=%s%s\n", result.Target.Model, result.Answer.TotalTokens, tokensPerSecond(result.Answer), result.Answer.Duration.Seconds(), costText, controlText)
	}
}

func requestCost(status requestStatus) (float64, bool) {
	if !status.Result.UsageKnown && status.Result.PromptTokens == 0 && status.Result.CompletionTokens == 0 {
		return 0, false
	}
	return agent.EstimateCost(status.BaseURL, status.Result)
}
