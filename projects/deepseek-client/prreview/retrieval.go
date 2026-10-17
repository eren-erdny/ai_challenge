package prreview

import (
	"crypto/sha256"
	"fmt"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

func chunkFile(p, text string) []Chunk {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	kind := "code"
	if strings.ToLower(path.Ext(p)) == ".md" || strings.ToLower(path.Ext(p)) == ".markdown" {
		kind = "documentation"
	}
	out := []Chunk{}
	for start := 0; start < len(lines); start += 30 {
		end := min(start+40, len(lines))
		content := strings.Join(lines[start:end], "\n")
		if len(content) > 16000 {
			continue
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", p, start+1, content)))
		out = append(out, Chunk{ID: fmt.Sprintf("%x", hash[:12]), Path: p, Kind: kind, Start: start + 1, End: end, Text: content})
		if end == len(lines) {
			break
		}
	}
	return out
}

var camel = regexp.MustCompile("([a-z0-9])([A-Z])")

func terms(text string) map[string]float64 {
	words := strings.FieldsFunc(strings.ToLower(camel.ReplaceAllString(text, "$1 $2")), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	counts := map[string]float64{}
	for _, word := range words {
		if len(word) < 2 {
			continue
		}
		switch word {
		case "func", "return", "var", "const", "package", "import", "the", "and", "this", "string", "nil", "true", "false":
			continue
		}
		counts[word]++
	}
	return counts
}

// TF-IDF cosine retrieval is deterministic vector RAG, not a neural embedder.
// Separate quotas ensure relevant documentation and code can both be selected.
func Retrieve(chunks []Chunk, query string) []Chunk {
	counts := make([]map[string]float64, len(chunks))
	df := map[string]int{}
	for i, c := range chunks {
		counts[i] = terms(c.Path + " " + c.Text)
		for word := range counts[i] {
			df[word]++
		}
	}
	weights := func(values map[string]float64) map[string]float64 {
		vector := map[string]float64{}
		norm := 0.0
		for word, count := range values {
			weight := (1 + math.Log(count)) * math.Log(1+float64(len(chunks))/float64(1+df[word]))
			vector[word] = weight
			norm += weight * weight
		}
		if norm > 0 {
			for word, w := range vector {
				vector[word] = w / math.Sqrt(norm)
			}
		}
		return vector
	}
	q := weights(terms(query))
	ranked := []Chunk{}
	for i, c := range chunks {
		score := 0.0
		for word, w := range weights(counts[i]) {
			score += w * q[word]
		}
		if score > 0 {
			c.Score = score
			ranked = append(ranked, c)
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			return ranked[i].ID < ranked[j].ID
		}
		return ranked[i].Score > ranked[j].Score
	})
	out := []Chunk{}
	limits := map[string]int{"documentation": 3, "code": 5}
	total := 0
	for _, c := range ranked {
		if limits[c.Kind] == 0 || total+len(c.Text) > 24000 {
			continue
		}
		limits[c.Kind]--
		total += len(c.Text)
		out = append(out, c)
	}
	return out
}
