package main

import (
	"strings"
	"testing"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/knowledge"
)

func TestRetrievalCommandsThroughTUI(t *testing.T) {
	config := defaultAppConfig()
	config.Knowledge = &knowledge.Manager{Dir: t.TempDir()}
	config.ConversationID = "settings-chat"
	model := newTUIModel(config, nil)
	for _, command := range []string{"/rag baseline", "/rag set 30 3", "/rag threshold 0.6", "/rag rewrite on"} {
		model.textarea.SetValue(command)
		next, _ := model.submit()
		model = next.(tuiModel)
	}
	got, err := config.Knowledge.Retrieval("settings-chat")
	if err != nil || got.Candidates != 30 || got.TopK != 3 || got.Rerank || !got.Rewrite || got.MinSimilarity == nil || *got.MinSimilarity != .6 {
		t.Fatalf("%+v %v", got, err)
	}
	model.textarea.SetValue("/rag set 2 10")
	next, _ := model.submit()
	model = next.(tuiModel)
	got, _ = config.Knowledge.Retrieval("settings-chat")
	if got.Candidates != 30 || !strings.Contains(strings.Join(model.history, "\n"), "не сохранены") {
		t.Fatal("invalid command changed persisted settings")
	}
	model.textarea.SetValue("/rag enhanced")
	next, _ = model.submit()
	model = next.(tuiModel)
	got, _ = config.Knowledge.Retrieval("settings-chat")
	if !got.Rerank || got.MinRerankScore == nil || *got.MinRerankScore != -3 {
		t.Fatal("enhanced preset not applied")
	}
	model.textarea.SetValue("/rag rerank off")
	model.submit()
	got, _ = config.Knowledge.Retrieval("settings-chat")
	if got.Rerank || got.MinRerankScore != nil {
		t.Fatal("rerank off retained incompatible score cutoff")
	}
}
