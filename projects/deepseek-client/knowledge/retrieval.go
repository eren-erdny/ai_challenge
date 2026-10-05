package knowledge

import (
	"fmt"
	"math"
)

// RetrievalOptions persist per conversation, independently of the selected base.
type RetrievalOptions struct {
	MinRerankScore *float64 `json:"min_rerank_score"`
	Candidates     int      `json:"candidates"`
	TopK           int      `json:"top_k"`
	Rerank         bool     `json:"rerank"`
	Rewrite        bool     `json:"rewrite"`
	MinSimilarity  *float64 `json:"min_similarity"`
}

func DefaultRetrieval() RetrievalOptions {
	threshold := 0.49
	score := -3.0
	return RetrievalOptions{Candidates: 20, TopK: 5, Rerank: true, Rewrite: true, MinSimilarity: &threshold, MinRerankScore: &score}
}

func (o RetrievalOptions) Validate() error {
	if o.TopK < 1 || o.TopK > 20 || o.Candidates < o.TopK || o.Candidates > 100 {
		return fmt.Errorf("нужно 1 <= top-K <= 20, top-K <= кандидаты <= 100")
	}
	if o.MinSimilarity != nil && (math.IsNaN(*o.MinSimilarity) || math.IsInf(*o.MinSimilarity, 0) || *o.MinSimilarity < -1 || *o.MinSimilarity > 1) {
		return fmt.Errorf("порог cosine должен быть числом от -1 до 1")
	}
	if o.MinRerankScore != nil && (!o.Rerank || math.IsNaN(*o.MinRerankScore) || math.IsInf(*o.MinRerankScore, 0)) {
		return fmt.Errorf("порог reranker требует включённый rerank и конечное число")
	}
	return nil
}

func (m *Manager) Retrieval(chat string) (RetrievalOptions, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return RetrievalOptions{}, err
	}
	return m.retrieval(chat), nil
}

func (m *Manager) retrieval(chat string) RetrievalOptions {
	if o, ok := m.data.Retrieval[chat]; ok {
		return cloneRetrieval(o)
	}
	return DefaultRetrieval()
}

func (m *Manager) SetRetrieval(chat string, o RetrievalOptions) error {
	if err := o.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return err
	}
	next := m.copyCatalog()
	next.Retrieval[chat] = cloneRetrieval(o)
	return m.save(next)
}

func cloneRetrieval(o RetrievalOptions) RetrievalOptions {
	if o.MinSimilarity != nil {
		value := *o.MinSimilarity
		o.MinSimilarity = &value
	}
	if o.MinRerankScore != nil {
		value := *o.MinRerankScore
		o.MinRerankScore = &value
	}
	return o
}
