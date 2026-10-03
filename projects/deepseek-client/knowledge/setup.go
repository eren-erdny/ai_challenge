package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func venvPythonPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join("Scripts", "python.exe")
	}
	return filepath.Join("bin", "python")
}
func findPython() (string, error) {
	for _, name := range []string{"python", "python3", "py"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	home, _ := os.UserHomeDir()
	candidate := filepath.Join(home, ".cache", "codex-runtimes", "codex-primary-runtime", "dependencies", "python", "python.exe")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", errors.New("установите Python 3.12+; затем повторите создание базы")
}
func findOllama() (string, error) {
	if path, err := exec.LookPath("ollama"); err == nil {
		return path, nil
	}
	candidate := filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Ollama", "ollama.exe")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", errors.New("установите Ollama: https://ollama.com/download; затем повторите создание базы")
}
func ollamaModels(ctx context.Context) ([]string, error) {
	httpClient := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}
	request, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:11434/api/tags", nil)
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, errors.New("Ollama unavailable")
	}
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err = json.NewDecoder(response.Body).Decode(&tags); err != nil {
		return nil, err
	}
	var names []string
	for _, model := range tags.Models {
		names = append(names, model.Name)
	}
	return names, nil
}
func (m *Manager) ensure(ctx context.Context, out io.Writer) (string, error) {
	if m.prepare != nil {
		return m.prepare(ctx, out)
	}
	ollama, err := findOllama()
	if err != nil {
		return "", err
	}
	runtimeDirectory := filepath.Join(m.Dir, "runtime")
	if m.Runtime == nil {
		return "", errors.New("RAG runtime is unavailable")
	}
	if err := fs.WalkDir(m.Runtime, ".", func(path string, item fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if item.IsDir() {
			return os.MkdirAll(filepath.Join(runtimeDirectory, filepath.FromSlash(path)), 0700)
		}
		value, err := fs.ReadFile(m.Runtime, path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(runtimeDirectory, filepath.FromSlash(path)), value, 0600)
	}); err != nil {
		return "", err
	}
	python := filepath.Join(m.Dir, ".venv", venvPythonPath())
	if m.Python != "" {
		python, err = filepath.Abs(m.Python)
		if err != nil {
			return "", err
		}
		if _, err = os.Stat(python); err != nil {
			return "", err
		}
	} else if _, err := os.Stat(python); errors.Is(err, os.ErrNotExist) {
		bootstrap, err := findPython()
		if err != nil {
			return "", err
		}
		fmt.Fprintln(out, "Первичная подготовка Python...")
		if err = m.run(ctx, bootstrap, []string{"-m", "venv", filepath.Join(m.Dir, ".venv")}, m.environment("setup"), runtimeDirectory, out); err != nil {
			return "", err
		}
	}
	requirements, err := os.ReadFile(filepath.Join(runtimeDirectory, "requirements.txt"))
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(requirements)
	expected := hex.EncodeToString(digest[:])
	marker := filepath.Join(m.Dir, ".venv", "dependencies-ready")
	installed, _ := os.ReadFile(marker)
	if m.Python != "" {
		if err = m.run(ctx, python, []string{"-c", "import qdrant_client, httpx, transformers, torch, pypdf, sentencepiece"}, m.environment("setup"), runtimeDirectory, out); err != nil {
			return "", fmt.Errorf("готовое окружение RAG не содержит нужные зависимости: %w", err)
		}
	} else if string(installed) != expected {
		fmt.Fprintln(out, "Первичная установка зависимостей RAG (может занять несколько минут)...")
		if err = m.run(ctx, python, []string{"-m", "pip", "install", "-r", "requirements.txt"}, m.environment("setup"), runtimeDirectory, out); err != nil {
			return "", err
		}
		if err = os.WriteFile(marker, []byte(expected), 0600); err != nil {
			return "", err
		}
	}
	names, err := ollamaModels(ctx)
	if err != nil {
		if m.ollama != nil {
			_ = m.ollama.Process.Kill()
			_ = m.ollama.Wait()
			m.ollama = nil
		}
		cmd := exec.Command(ollama, "serve")
		cmd.Env = m.environment("setup")
		hideProcess(cmd)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if err = cmd.Start(); err != nil {
			return "", err
		}
		m.ollama = cmd
		for attempt := 0; attempt < 40; attempt++ {
			names, err = ollamaModels(ctx)
			if err == nil {
				break
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
		if err != nil {
			return "", fmt.Errorf("не удалось запустить Ollama: %w", err)
		}
	}
	found := false
	for _, name := range names {
		if name == "nomic-embed-text:latest" {
			found = true
		}
	}
	if !found {
		fmt.Fprintln(out, "Загрузка embedding-модели...")
		env := m.environment("setup")
		if err = m.run(ctx, ollama, []string{"pull", "nomic-embed-text"}, env, runtimeDirectory, out); err != nil {
			return "", err
		}
	}
	modelMarker := filepath.Join(m.Dir, "models-ready")
	cacheKey := m.ModelCache
	if cacheKey == "" {
		cacheKey = filepath.Join(m.Dir, "models")
	}
	preparedCache, _ := os.ReadFile(modelMarker)
	if string(preparedCache) != cacheKey {
		args := []string{"-m", "rag", "download-models"}
		if m.ModelCache != "" {
			fmt.Fprintln(out, "Проверка готового локального кэша RAG...")
			args = []string{"-c", "from rag.models import CACHE, Reranker; from rag.chunking import TokenCounter; TokenCounter(cache_dir=str(CACHE)); Reranker()"}
		} else {
			fmt.Fprintln(out, "Первичная загрузка токенизатора и reranker...")
		}
		if err = m.run(ctx, python, args, m.environment("setup"), runtimeDirectory, out); err != nil {
			return "", err
		}
		if err = os.WriteFile(modelMarker, []byte(cacheKey), 0600); err != nil {
			return "", err
		}
	}
	return python, nil
}
