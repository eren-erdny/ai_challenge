package support

import (
	"crypto/sha256"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

type Chunk struct {
	ID      string   `json:"chunk_id"`
	Source  string   `json:"source"`
	Section string   `json:"section"`
	Text    string   `json:"text"`
	Score   *float64 `json:"vector_score,omitempty"`
}
type Corpus struct{ Chunks []Chunk }

func LoadCorpus(dir string) (Corpus, error) {
	corpus := Corpus{}
	entries, e := os.ReadDir(dir)
	if e != nil {
		return corpus, e
	}
	total := 0
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || strings.ToLower(filepath.Ext(entry.Name())) != ".md" {
			continue
		}
		info, e := entry.Info()
		if e != nil {
			return corpus, e
		}
		if info.Size() > 64<<10 {
			return corpus, fmt.Errorf("documentation too large")
		}
		b, e := os.ReadFile(filepath.Join(dir, entry.Name()))
		if e != nil {
			return corpus, e
		}
		total += len(b)
		if total > 512<<10 {
			return corpus, fmt.Errorf("corpus too large")
		}
		title := entry.Name()
		var body strings.Builder
		flush := func() {
			text := strings.TrimSpace(body.String())
			body.Reset()
			if text == "" {
				return
			}
			hash := sha256.Sum256([]byte(entry.Name() + "\n" + title + "\n" + text))
			corpus.Chunks = append(corpus.Chunks, Chunk{ID: fmt.Sprintf("faq-%x", hash[:10]), Source: "faq/" + entry.Name(), Section: title, Text: text})
		}
		for _, line := range strings.Split(strings.TrimPrefix(string(b), "\ufeff"), "\n") {
			if strings.HasPrefix(line, "#") {
				flush()
				title = strings.TrimSpace(strings.TrimLeft(line, "# "))
				continue
			}
			body.WriteString(line + "\n")
		}
		flush()
	}
	if len(corpus.Chunks) == 0 {
		return corpus, fmt.Errorf("empty FAQ corpus")
	}
	return corpus, nil
}
func tokens(text string) map[string]float64 {
	result := map[string]float64{}
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' }) {
		if len([]rune(word)) < 3 {
			continue
		}
		switch word {
		case "как", "что", "почему", "это", "для", "или", "при", "the", "and", "with":
			continue
		}
		if len([]rune(word)) > 5 && !strings.Contains(word, "_") {
			for _, suffix := range []string{"иями", "ения", "ении", "ация", "ации", "ами", "ого", "ому", "ить", "ать", "ый", "ий", "ая", "ые", "ов", "ом", "ам", "ах", "а", "я", "ы", "и", "е", "у"} {
				if strings.HasSuffix(word, suffix) {
					word = strings.TrimSuffix(word, suffix)
					break
				}
			}
		}
		result[word]++
	}
	return result
}

// Sparse TF-IDF vectors + cosine retrieval; no neural model or external index service.
func (c Corpus) Search(query string) []Chunk {
	df := map[string]int{}
	values := make([]map[string]float64, len(c.Chunks))
	for i, chunk := range c.Chunks {
		values[i] = tokens(chunk.Section + " " + chunk.Text)
		for w := range values[i] {
			df[w]++
		}
	}
	weights := func(counts map[string]float64) map[string]float64 {
		out := map[string]float64{}
		norm := 0.0
		for w, n := range counts {
			if df[w] == 0 {
				continue
			}
			v := (1 + math.Log(n)) * math.Log(1+float64(len(c.Chunks))/float64(1+df[w]))
			out[w] = v
			norm += v * v
		}
		if norm > 0 {
			for w, v := range out {
				out[w] = v / math.Sqrt(norm)
			}
		}
		return out
	}
	q := weights(tokens(query))
	result := []Chunk{}
	for i, chunk := range c.Chunks {
		score := 0.0
		for w, v := range weights(values[i]) {
			score += v * q[w]
		}
		if score >= .10 {
			value := score
			chunk.Score = &value
			result = append(result, chunk)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if *result[i].Score == *result[j].Score {
			return result[i].ID < result[j].ID
		}
		return *result[i].Score > *result[j].Score
	})
	return result[:min(4, len(result))]
}
