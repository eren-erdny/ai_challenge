package knowledge

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AttachIndex registers an explicitly chosen local index without rebuilding or
// copying it. Only search opens the original Qdrant store. Updates create a new
// managed version and never write document/vector changes to the borrowed index.
func (m *Manager) AttachIndex(name, manifestPath, chat string) (Base, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return Base{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return Base{}, errors.New("name must contain 1..80 characters")
	}
	path, err := sourcePath(manifestPath)
	if err != nil {
		return Base{}, err
	}
	if filepath.Base(path) != "index.json" {
		return Base{}, errors.New("choose the existing index.json manifest")
	}
	var manifest struct {
		Backend     string                     `json:"backend"`
		Qdrant      string                     `json:"qdrant_path"`
		Sources     []string                   `json:"source_roots"`
		Collections map[string]json.RawMessage `json:"collections"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Base{}, err
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		return Base{}, err
	}
	dir := filepath.Dir(path)
	if manifest.Backend != "local" || filepath.Clean(manifest.Qdrant) != filepath.Join(dir, "qdrant") || len(manifest.Collections["structured"]) == 0 || len(manifest.Sources) == 0 {
		return Base{}, errors.New("requires a local index with structured collection, source_roots and adjacent qdrant store")
	}
	info, err := os.Stat(filepath.Join(dir, "qdrant"))
	if err != nil || !info.IsDir() {
		return Base{}, errors.New("local qdrant directory is unavailable")
	}
	next := m.copyCatalog()
	for id, base := range next.Bases {
		if base.IndexDir == dir {
			next.Chats[chat] = id
			return base, m.save(next)
		}
		if strings.EqualFold(base.Name, name) {
			name = fmt.Sprintf("%s (%s)", name, filepath.Base(dir))
			for _, other := range next.Bases {
				if strings.EqualFold(other.Name, name) {
					return Base{}, errors.New("a base with this name already exists")
				}
			}
		}
	}
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		return Base{}, err
	}
	base := Base{ID: hex.EncodeToString(random), Name: name, Sources: manifest.Sources, IndexDir: dir, Ready: true, Updated: time.Now().UTC().Format(time.RFC3339)}
	next.Bases[base.ID] = base
	next.Chats[chat] = base.ID
	if err = m.save(next); err != nil {
		return Base{}, err
	}
	return base, nil
}
