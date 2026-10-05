package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/ragtools"
)

func handleRAGCommand(args []string, output, errorOutput io.Writer) (bool, int) {
	if len(args) == 0 || (args[0] != "--rag-search" && args[0] != "--rag-status") {
		return false, 0
	}
	if (args[0] == "--rag-status" && len(args) != 1) || (args[0] == "--rag-search" && len(args) < 2) {
		fmt.Fprintln(errorOutput, "Использование: --rag-status | --rag-search ВОПРОС")
		return true, 2
	}
	address := os.Getenv("RAG_URL")
	if address == "" {
		address = "http://127.0.0.1:8765"
	}
	client, err := ragtools.New(address, nil)
	var value string
	if err == nil {
		if args[0] == "--rag-status" {
			value, err = client.Status(context.Background())
		} else {
			body, _ := json.Marshal(map[string]string{"query": strings.Join(args[1:], " ")})
			value, err = client.Execute(context.Background(), "rag_search", string(body))
		}
	}
	if err != nil {
		fmt.Fprintln(errorOutput, err)
		return true, 1
	}
	fmt.Fprintln(output, value)
	return true, 0
}
