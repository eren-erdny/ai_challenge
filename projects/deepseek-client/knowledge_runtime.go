package main

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/knowledge"
)

// Embedding keeps ordinary compiled-client startup independent of the checkout.
//
//go:embed rag-service/rag/*.py rag-service/qdrant_store.py rag-service/requirements.txt
var knowledgeRuntime embed.FS

func newKnowledgeManager(configDirectory string) *knowledge.Manager {
	source, _ := fs.Sub(knowledgeRuntime, "rag-service")
	return &knowledge.Manager{Dir: filepath.Join(configDirectory, "knowledge"), Runtime: source, Python: os.Getenv("DEEPSEEK_RAG_PYTHON"), ModelCache: os.Getenv("DEEPSEEK_RAG_MODEL_CACHE")}
}
