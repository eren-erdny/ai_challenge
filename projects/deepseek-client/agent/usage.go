package agent

import (
	"context"
	"time"
)

// TurnUsage records provider usage, not a re-tokenization of saved messages.
type TurnUsage struct {
	Model       string        `json:"model"`
	UsageKnown  bool          `json:"usage_known"`
	Input       int           `json:"input_tokens"`
	CachedInput int           `json:"cached_input_tokens"`
	Output      int           `json:"output_tokens"`
	Total       int           `json:"total_tokens"`
	CostUSD     *float64      `json:"estimated_cost_usd"`
	Duration    time.Duration `json:"duration_ns"`
}

// TurnStore commits the messages and accounting in the same atomic write.
type TurnStore interface {
	SaveTurn(context.Context, string, []Message, TurnUsage) error
}

type ConversationUsage struct {
	Turns             int
	UnknownUsageTurns int
	UnknownCostTurns  int
	Input             int
	CachedInput       int
	Output            int
	Total             int
	CostUSD           float64
}

type UsageStore interface {
	Usage(context.Context, string) (ConversationUsage, error)
}
