package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Opt-in: actual local embeddings and models; never a paid provider call.
func TestKnowledgeUIRealLocalRAG(t *testing.T) {
	if os.Getenv("DEEPSEEK_RAG_LIVE_TEST") != "1" {
		t.Skip("set DEEPSEEK_RAG_LIVE_TEST=1 with a prepared local Ollama/backend")
	}
	if os.Getenv("DEEPSEEK_RAG_PYTHON") == "" || os.Getenv("DEEPSEEK_RAG_MODEL_CACHE") == "" {
		t.Fatal("provide the existing backend Python and model cache")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	root := t.TempDir()
	docs := filepath.Join(root, "documents with spaces")
	if err := os.Mkdir(docs, 0700); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(docs, "operations.md")
	if err := os.WriteFile(first, []byte("# Backup policy\nBackup copies are retained for exactly fourteen days.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(root, "other.md")
	os.WriteFile(second, []byte("# Backup policy\nBackup copies are retained for exactly ninety days.\n"), 0600)
	extra := filepath.Join(root, "audit.md")
	os.WriteFile(extra, []byte("# Audit\nAudit records are stored in the dedicated archive called Lighthouse.\n"), 0600)
	manager := newKnowledgeManager(root)
	defer manager.Close()
	config := defaultAppConfig()
	config.Knowledge = manager
	config.ConversationID = "integration-chat"
	config.ResponseControl.Enabled = false
	expected := "fourteen days"
	modelCalls := 0
	ask := func(_ context.Context, _ string, _ string, settings requestSettings) (completionResult, error) {
		modelCalls++
		found := false
		for _, message := range settings.Messages {
			if message.Role == "user" && strings.Contains(message.Content, "Current knowledge-base search results") {
				if !strings.Contains(message.Content, expected) {
					return completionResult{}, fmt.Errorf("wrong evidence: expected %s", expected)
				}
				if expected == "ninety days" && strings.Contains(message.Content, "fourteen days") {
					return completionResult{}, fmt.Errorf("other base leaked into retrieval")
				}
				found = true
			}
		}
		if !found {
			return completionResult{}, fmt.Errorf("retrieval evidence did not reach the model")
		}
		return completionResult{Content: "Scripted answer using verified evidence: " + expected, Model: "scripted-local-fixture"}, nil
	}
	model := newTUIModel(config, ask)
	model.ctx = ctx
	createThroughUI := func(name, path string) {
		t.Helper()
		opened, _ := model.openKnowledge()
		model = opened.(tuiModel)
		changed, _ := model.submitKnowledge("1")
		model = changed.(tuiModel)
		changed, _ = model.submitKnowledge(name)
		model = changed.(tuiModel)
		changed, command := model.submitKnowledge(path)
		model = changed.(tuiModel)
		batch, ok := command().(tea.BatchMsg)
		if !ok {
			t.Fatal("indexing is not an asynchronous UI job")
		}
		result := batch[0]()
		message, ok := result.(knowledgeMessage)
		if !ok {
			t.Fatal("missing knowledge job response")
		}
		if !strings.Contains(message.text, "База знаний готова") {
			t.Fatal(message.text)
		}
		changed, _ = model.Update(message)
		model = changed.(tuiModel)
		if model.state.KnowledgeLabel != name {
			t.Fatalf("selected label %q", model.state.KnowledgeLabel)
		}
	}
	queryThroughClient := func(query string) {
		t.Helper()
		response := executeQuestionCommand(ctx, "", query, model.state, ask)().(answerMessage)
		if !strings.Contains(response.text, "Scripted answer using verified evidence: "+expected) || !strings.Contains(response.text, "найдено фрагментов") || response.lastStatus == nil {
			t.Fatalf("client flow failed:\n%s", response.text)
		}
	}
	createThroughUI("Alpha", docs)
	queryThroughClient("How many days are backup copies retained?")
	alpha, _ := manager.Selected("integration-chat")
	createThroughUI("Beta", second)
	expected = "ninety days"
	queryThroughClient("How many days are backup copies retained?")
	beta, _ := manager.Selected("integration-chat")
	if alpha.StorageID == beta.StorageID {
		t.Fatal("shared database")
	}
	opened, _ := model.openKnowledge()
	model = opened.(tuiModel)
	changed, _ := model.submitKnowledge("2")
	model = changed.(tuiModel)
	changed, _ = model.submitKnowledge("1")
	model = changed.(tuiModel)
	expected = "fourteen days"
	queryThroughClient("How many days are backup copies retained?")
	result := knowledgeJob(ctx, model.state, "add", "", extra)
	if !strings.Contains(result, "База знаний готова") {
		t.Fatal(result)
	}
	expected = "Lighthouse"
	queryThroughClient("Where are audit records stored?")
	result = knowledgeJob(ctx, model.state, "remove", "", "2")
	if !strings.Contains(result, "База знаний готова") {
		t.Fatal(result)
	}
	expected = "fourteen days"
	queryThroughClient("How many days are backup copies retained?")
	os.WriteFile(first, []byte("# Backup policy\nBackup copies are retained for exactly twenty-one days.\n"), 0600)
	result = knowledgeJob(ctx, model.state, "update", "", "")
	if !strings.Contains(result, "База знаний готова") {
		t.Fatal(result)
	}
	expected = "twenty-one days"
	queryThroughClient("How many days are backup copies retained?")
	manager.Close()
	reopened := newKnowledgeManager(root)
	defer reopened.Close()
	model.state.Knowledge = reopened
	refreshKnowledgeLabel(&model.state)
	if model.state.KnowledgeLabel != "Alpha" {
		t.Fatal("selection lost after restart")
	}
	queryThroughClient("How many days are backup copies retained?")
	if err := reopened.Select("other-chat", beta.ID); err != nil {
		t.Fatal(err)
	}
	restored, _ := reopened.Selected("integration-chat")
	if restored.ID != alpha.ID {
		t.Fatal("chat selections are mixed")
	}
	if err := reopened.Select("integration-chat", ""); err != nil {
		t.Fatal(err)
	}
	refreshKnowledgeLabel(&model.state)
	if evidence, err := reopened.Search(ctx, "integration-chat", "question"); err != nil || evidence != "" {
		t.Fatal("disabled base still queried")
	}
	if modelCalls != 7 {
		t.Fatalf("unexpected generation count %d", modelCalls)
	}
	report := map[string]any{"result": "PASS", "backend": "real Ollama + Qdrant Local + cached multilingual reranker", "generation": "scripted fixture; no provider request", "ui": "TUI wizard and asynchronous index command", "checks": []string{"create two bases", "automatic evidence in model input", "switch back", "add source", "remove source", "update source", "restart restores selection", "independent chat selection", "disable retrieval"}, "scripted_model_calls": modelCalls, "user_documents_used": false}
	if output := os.Getenv("DEEPSEEK_RAG_TEST_REPORT"); output != "" {
		encoded, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(output, encoded, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("PASS: real local RAG through UI jobs and client adapter; 7 scripted generations, no paid requests")
}
