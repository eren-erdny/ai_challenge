package main

import (
	"fmt"
	"io"
	"strconv"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/knowledge"
)

func handleRetrievalSettings(parts []string, state *sessionState, out io.Writer) {
	if state.Knowledge == nil {
		fmt.Fprintln(out, "Базы знаний недоступны.")
		return
	}
	chat := conversationID(state.ConversationID)
	o, err := state.Knowledge.Retrieval(chat)
	if err != nil {
		fmt.Fprintln(out, err)
		return
	}
	switch {
	case len(parts) == 1:
	case len(parts) == 2 && parts[1] == "baseline":
		o = knowledge.RetrievalOptions{Candidates: 20, TopK: 5}
	case len(parts) == 2 && parts[1] == "enhanced":
		o = knowledge.DefaultRetrieval()
	case len(parts) == 4 && parts[1] == "set":
		o.Candidates, err = strconv.Atoi(parts[2])
		if err == nil {
			o.TopK, err = strconv.Atoi(parts[3])
		}
	case len(parts) == 3 && (parts[1] == "threshold" || parts[1] == "score-threshold"):
		var cutoff *float64
		if parts[2] == "off" {
			cutoff = nil
		} else {
			var threshold float64
			threshold, err = strconv.ParseFloat(parts[2], 64)
			cutoff = &threshold
		}
		if parts[1] == "threshold" {
			o.MinSimilarity = cutoff
		} else {
			o.MinRerankScore = cutoff
		}
	case len(parts) == 3 && (parts[1] == "rewrite" || parts[1] == "rerank") && (parts[2] == "on" || parts[2] == "off"):
		if parts[1] == "rewrite" {
			o.Rewrite = parts[2] == "on"
		} else {
			o.Rerank = parts[2] == "on"
			if !o.Rerank {
				o.MinRerankScore = nil
			}
		}
	default:
		fmt.Fprintln(out, "/rag [baseline|enhanced|set КАНДИДАТЫ TOP-K|threshold ЧИСЛО/off|score-threshold ЧИСЛО/off|rewrite on/off|rerank on/off]")
		return
	}
	if err == nil && len(parts) > 1 {
		err = state.Knowledge.SetRetrieval(chat, o)
	}
	if err != nil {
		fmt.Fprintln(out, "Настройки RAG не сохранены:", err)
		return
	}
	threshold := "off"
	if o.MinSimilarity != nil {
		threshold = fmt.Sprintf("%.3f", *o.MinSimilarity)
	}
	score := "off"
	if o.MinRerankScore != nil {
		score = fmt.Sprintf("%.3f", *o.MinRerankScore)
	}
	fmt.Fprintf(out, "RAG этого чата: кандидаты=%d → top-K=%d, cosine ≥ %s, rerank=%t (score ≥ %s), rewrite=%t. База выбирается через F2 / /kb.\n", o.Candidates, o.TopK, threshold, o.Rerank, score, o.Rewrite)
}
