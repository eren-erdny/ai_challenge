package agent

import (
	"net/url"
	"strings"
)

type tokenPricing struct {
	InputPerMillion       float64
	CachedInputPerMillion float64
	OutputPerMillion      float64
}

func pricingFor(baseURL string, model string) (tokenPricing, bool) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return tokenPricing{}, false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return tokenPricing{}, true
	}

	model = strings.ToLower(strings.TrimSpace(model))
	if host == "api.deepseek.com" {
		switch model {
		case "deepseek-v4-flash":
			return tokenPricing{InputPerMillion: 0.14, CachedInputPerMillion: 0.0028, OutputPerMillion: 0.28}, true
		case "deepseek-v4-pro":
			return tokenPricing{InputPerMillion: 0.435, CachedInputPerMillion: 0.003625, OutputPerMillion: 0.87}, true
		}
	}
	if host == "ollama.com" {
		switch model {
		case "gpt-oss:120b", "gpt-oss:120b-cloud":
			return tokenPricing{InputPerMillion: 0.15, CachedInputPerMillion: 0.014, OutputPerMillion: 0.60}, true
		case "gpt-oss:20b", "gpt-oss:20b-cloud":
			return tokenPricing{InputPerMillion: 0.07, CachedInputPerMillion: 0.035, OutputPerMillion: 0.30}, true
		}
	}
	return tokenPricing{}, false
}
func tokensPerSecond(result Completion) float64 {
	if result.Duration <= 0 {
		return 0
	}
	return float64(result.CompletionTokens) / result.Duration.Seconds()
}

func EstimateCost(baseURL string, result Completion) (float64, bool) {
	pricing, ok := pricingFor(baseURL, result.Model)
	if !ok {
		return 0, false
	}
	cached := max(0, min(result.CachedInputTokens, result.PromptTokens))
	return (float64(max(0, result.PromptTokens-cached))*pricing.InputPerMillion +
		float64(cached)*pricing.CachedInputPerMillion +
		float64(result.CompletionTokens)*pricing.OutputPerMillion) / 1_000_000, true
}
