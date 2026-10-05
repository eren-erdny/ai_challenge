package knowledge

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Base struct {
	StorageID string   `json:"storage_id,omitempty"`
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Sources   []string `json:"sources"`
	Ready     bool     `json:"ready"`
	Updated   string   `json:"updated,omitempty"`
}
type catalog struct {
	Retrieval map[string]RetrievalOptions `json:"retrieval,omitempty"`
	Bases     map[string]Base             `json:"bases"`
	Chats     map[string]string           `json:"chats"`
}
type Manager struct {
	Dir     string
	Runtime fs.FS
	// Optional existing backend environment/cache; never install into it.
	Python     string
	ModelCache string
	mu         sync.Mutex
	data       catalog
	loaded     bool
	worker     *exec.Cmd
	input      io.WriteCloser
	output     *bufio.Scanner
	workerID   string
	ollama     *exec.Cmd
	// Run is replaceable for local tests; production uses a real child process.
	Run     func(context.Context, string, []string, []string, string, io.Writer) error
	prepare func(context.Context, io.Writer) (string, error)
}

func (m *Manager) load() error {
	if m.loaded {
		return nil
	}
	m.data = catalog{Bases: map[string]Base{}, Chats: map[string]string{}, Retrieval: map[string]RetrievalOptions{}}
	value, err := os.ReadFile(filepath.Join(m.Dir, "catalog.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal(value, &m.data); err != nil {
			return fmt.Errorf("knowledge catalog: %w", err)
		}
		if m.data.Bases == nil {
			m.data.Bases = map[string]Base{}
		}
		if m.data.Chats == nil {
			m.data.Chats = map[string]string{}
		}
		if m.data.Retrieval == nil {
			m.data.Retrieval = map[string]RetrievalOptions{}
		}
		for _, o := range m.data.Retrieval {
			if err := o.Validate(); err != nil {
				return err
			}
		}
		for id, base := range m.data.Bases {
			if len(id) != 32 || base.ID != id {
				return errors.New("invalid knowledge base ID")
			}
			if _, err := hex.DecodeString(id); err != nil {
				return err
			}
			if base.StorageID != "" {
				if len(base.StorageID) != 32 {
					return errors.New("invalid knowledge storage ID")
				}
				if _, err := hex.DecodeString(base.StorageID); err != nil {
					return err
				}
			}
		}
	}
	m.loaded = true
	return nil
}
func (m *Manager) save(next catalog) error {
	if err := os.MkdirAll(m.Dir, 0700); err != nil {
		return err
	}
	value, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(m.Dir, "catalog-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(value)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(file.Name(), filepath.Join(m.Dir, "catalog.json")); err != nil {
		return err
	}
	m.data = next
	return nil
}
func (m *Manager) copyCatalog() catalog {
	next := catalog{Bases: map[string]Base{}, Chats: map[string]string{}, Retrieval: map[string]RetrievalOptions{}}
	for id, b := range m.data.Bases {
		b.Sources = append([]string(nil), b.Sources...)
		next.Bases[id] = b
	}
	for chat, id := range m.data.Chats {
		next.Chats[chat] = id
	}
	for chat, o := range m.data.Retrieval {
		next.Retrieval[chat] = o
	}
	return next
}
func (m *Manager) List() ([]Base, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return nil, err
	}
	var bases []Base
	for _, b := range m.data.Bases {
		b.Sources = append([]string(nil), b.Sources...)
		bases = append(bases, b)
	}
	sort.Slice(bases, func(i, j int) bool { return bases[i].Name < bases[j].Name })
	return bases, nil
}
func (m *Manager) Selected(chat string) (Base, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return Base{}, err
	}
	b := m.data.Bases[m.data.Chats[chat]]
	b.Sources = append([]string(nil), b.Sources...)
	return b, nil
}
func (m *Manager) Select(chat, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return err
	}
	if id != "" {
		b, ok := m.data.Bases[id]
		if !ok || !b.Ready {
			return errors.New("base is not indexed")
		}
	}
	next := m.copyCatalog()
	if id == "" {
		delete(next.Chats, chat)
	} else {
		next.Chats[chat] = id
	}
	return m.save(next)
}
func sourcePath(source string) (string, error) {
	if strings.TrimSpace(strings.Trim(source, "\"")) == "" {
		return "", errors.New("choose a document file or folder explicitly")
	}
	path, err := filepath.Abs(strings.Trim(strings.TrimSpace(source), "\""))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return "", errors.New("choose a regular file or folder")
	}
	return path, nil
}
func (m *Manager) Create(ctx context.Context, name, source, chat string, out io.Writer) (Base, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return Base{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return Base{}, errors.New("name must contain 1..80 characters")
	}
	for _, b := range m.data.Bases {
		if strings.EqualFold(b.Name, name) {
			return Base{}, errors.New("a base with this name already exists")
		}
	}
	path, err := sourcePath(source)
	if err != nil {
		return Base{}, err
	}
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		return Base{}, err
	}
	base := Base{ID: hex.EncodeToString(random), Name: name, Sources: []string{path}}
	if err = m.build(ctx, &base, out); err != nil {
		return Base{}, err
	}
	next := m.copyCatalog()
	next.Bases[base.ID] = base
	next.Chats[chat] = base.ID
	if err = m.save(next); err != nil {
		return Base{}, err
	}
	return base, nil
}
func (m *Manager) Update(ctx context.Context, id, source string, out io.Writer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return err
	}
	base, ok := m.data.Bases[id]
	if !ok {
		return errors.New("unknown base")
	}
	base.Sources = append([]string(nil), base.Sources...)
	if source != "" {
		path, err := sourcePath(source)
		if err != nil {
			return err
		}
		for _, existing := range base.Sources {
			if existing == path {
				return errors.New("source already added; use update instead")
			}
		}
		base.Sources = append(base.Sources, path)
	}
	if err := m.build(ctx, &base, out); err != nil {
		return err
	}
	next := m.copyCatalog()
	next.Bases[id] = base
	return m.save(next)
}
func (m *Manager) RemoveSource(ctx context.Context, id string, number int, out io.Writer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return err
	}
	base, ok := m.data.Bases[id]
	if !ok {
		return errors.New("unknown base")
	}
	if len(base.Sources) <= 1 {
		return errors.New("keep at least one source or choose another base")
	}
	if number < 1 || number > len(base.Sources) {
		return errors.New("invalid source number")
	}
	base.Sources = append(append([]string(nil), base.Sources[:number-1]...), base.Sources[number:]...)
	if err := m.build(ctx, &base, out); err != nil {
		return err
	}
	next := m.copyCatalog()
	next.Bases[id] = base
	return m.save(next)
}
func (m *Manager) environment(id string) []string {
	env := os.Environ()
	keys := []string{"RAG_DATA_DIR", "RAG_MODEL_CACHE", "QDRANT_URL", "OLLAMA_URL", "OLLAMA_HOST", "RAG_EMBEDDING_MODEL", "PYTHONUTF8"}
	filtered := env[:0]
	for _, item := range env {
		keep := true
		for _, key := range keys {
			if strings.HasPrefix(strings.ToUpper(item), key+"=") {
				keep = false
			}
		}
		if keep {
			filtered = append(filtered, item)
		}
	}
	cache := m.ModelCache
	if cache == "" {
		cache = filepath.Join(m.Dir, "models")
	}
	return append(filtered, "RAG_DATA_DIR="+filepath.Join(m.Dir, "bases", id), "RAG_MODEL_CACHE="+cache, "OLLAMA_URL=http://127.0.0.1:11434", "OLLAMA_HOST=127.0.0.1:11434", "RAG_EMBEDDING_MODEL=nomic-embed-text:latest", "PYTHONUTF8=1")
}
func (m *Manager) run(ctx context.Context, exe string, args, env []string, dir string, out io.Writer) error {
	if m.Run != nil {
		return m.Run(ctx, exe, args, env, dir, out)
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	hideProcess(cmd)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(exe), err)
	}
	return nil
}
func (m *Manager) build(ctx context.Context, base *Base, out io.Writer) error {
	m.stopWorker()
	python, err := m.ensure(ctx, out)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Индексация базы «%s»...\n", base.Name)
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		return err
	}
	storageID := hex.EncodeToString(random)
	args := []string{"-m", "rag", "index", base.Sources[0]}
	for _, source := range base.Sources[1:] {
		args = append(args, "--add-source", source)
	}
	if err = m.run(ctx, python, args, m.environment(storageID), filepath.Join(m.Dir, "runtime"), out); err != nil {
		return err
	}
	base.Ready = true
	base.StorageID = storageID
	base.Updated = time.Now().UTC().Format(time.RFC3339)
	return nil
}
func (m *Manager) stopWorker() {
	if m.worker != nil {
		_ = m.input.Close()
		_ = m.worker.Process.Kill()
		_ = m.worker.Wait()
		m.worker = nil
		m.workerID = ""
	}
}
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopWorker()
	if m.ollama != nil {
		_ = m.ollama.Process.Kill()
		_ = m.ollama.Wait()
		m.ollama = nil
	}
}
func (m *Manager) Search(ctx context.Context, chat, query string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return "", err
	}
	id := m.data.Chats[chat]
	if id == "" {
		return "", nil
	}
	base, ok := m.data.Bases[id]
	if !ok || !base.Ready {
		return "", errors.New("selected base is unavailable")
	}
	if base.StorageID != "" {
		id = base.StorageID
	}
	if m.workerID != id {
		m.stopWorker()
		python, err := m.ensure(ctx, io.Discard)
		if err != nil {
			return "", err
		}
		cmd := exec.Command(python, "-u", "-m", "rag.worker")
		hideProcess(cmd)
		cmd.Env = m.environment(id)
		cmd.Dir = filepath.Join(m.Dir, "runtime")
		input, err := cmd.StdinPipe()
		if err != nil {
			return "", err
		}
		output, err := cmd.StdoutPipe()
		if err != nil {
			input.Close()
			return "", err
		}
		cmd.Stderr = io.Discard
		if err = cmd.Start(); err != nil {
			input.Close()
			return "", err
		}
		m.worker = cmd
		m.input = input
		m.output = bufio.NewScanner(output)
		m.output.Buffer(make([]byte, 4096), 1<<20)
		m.workerID = id
	}
	options := m.retrieval(chat)
	value, _ := json.Marshal(struct {
		Query string `json:"query"`
		RetrievalOptions
	}{query, options})
	if _, err := fmt.Fprintln(m.input, string(value)); err != nil {
		m.stopWorker()
		return "", err
	}
	type reply struct {
		line []byte
		err  error
	}
	ready := make(chan reply, 1)
	go func() {
		if m.output.Scan() {
			ready <- reply{line: append([]byte(nil), m.output.Bytes()...)}
		} else {
			err := m.output.Err()
			if err == nil {
				err = errors.New("RAG worker stopped")
			}
			ready <- reply{err: err}
		}
	}()
	select {
	case <-ctx.Done():
		m.stopWorker()
		<-ready
		return "", ctx.Err()
	case r := <-ready:
		if r.err != nil {
			m.stopWorker()
			return "", r.err
		}
		var envelope struct {
			Result json.RawMessage `json:"result"`
			Error  string          `json:"error"`
		}
		if err := json.Unmarshal(r.line, &envelope); err != nil {
			return "", err
		}
		if envelope.Error != "" {
			return "", errors.New(envelope.Error)
		}
		if len(envelope.Result) == 0 {
			return "", errors.New("empty RAG response")
		}
		return string(envelope.Result), nil
	}
}
