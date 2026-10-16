package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/llm"
	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/prreview"
)

func main() { os.Exit(run(os.Args[1:])) }
func run(args []string) int {
	fs := flag.NewFlagSet("review-pr", flag.ContinueOnError)
	repo := fs.String("repo", ".", "Git repository")
	base := fs.String("base", "", "full trusted base SHA")
	head := fs.String("head", "", "full PR head SHA")
	scope := fs.String("scope", "projects/deepseek-client", "relative scope")
	output := fs.String("output", "review-output", "fresh output directory")
	budget := fs.Float64("budget-usd", 0, "explicit budget <= 0.20 USD")
	ledgerPath := fs.String("ledger", "", "durable budget ledger")
	dry := fs.Bool("dry-run", false, "collect without API call")
	if fs.Parse(args) != nil {
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(os.Stderr, err); return 1 }
	if !*dry && (*budget <= 0 || *budget > .20 || math.IsNaN(*budget) || math.IsInf(*budget, 0)) {
		return fail(fmt.Errorf("explicit budget must be in (0, 0.20]"))
	}
	if err := os.Mkdir(*output, 0700); err != nil {
		return fail(fmt.Errorf("output must be fresh: %w", err))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	input, _, err := prreview.Collect(ctx, *repo, *base, *head, *scope)
	if err != nil {
		return fail(err)
	}
	if err = prreview.WriteJSON(filepath.Join(*output, "input.json"), input); err != nil {
		return fail(err)
	}
	if *dry {
		fmt.Printf("DRY RUN: %d files, %d base chunks, %d retrieved; no API call\n", len(input.Files), input.CorpusChunks, len(input.Context))
		return 0
	}
	if len(input.Files) == 0 || len(input.Context) == 0 {
		return fail(fmt.Errorf("no reviewable diff or retrieved context"))
	}
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		return fail(fmt.Errorf("DEEPSEEK_API_KEY required"))
	}
	if *ledgerPath == "" {
		*ledgerPath = *output + ".budget.json"
	}
	lock, err := os.OpenFile(*ledgerPath+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fail(fmt.Errorf("cannot lock budget: %w", err))
	}
	lock.Close()
	defer os.Remove(*ledgerPath + ".lock")
	ledger := prreview.Ledger{Budget: *budget}
	data, err := os.ReadFile(*ledgerPath)
	if err == nil {
		if json.Unmarshal(data, &ledger) != nil || ledger.Budget != *budget {
			return fail(fmt.Errorf("existing ledger budget mismatch or invalid JSON"))
		}
	} else if !os.IsNotExist(err) {
		return fail(err)
	}
	client := llm.New(&http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, map[string]string{"deepseek": key})
	review, completion, err := prreview.Generate(ctx, client, input, &ledger, *ledgerPath)
	if e := prreview.WriteJSON(filepath.Join(*output, "completion.json"), completion); e != nil {
		return fail(e)
	}
	if e := prreview.WriteJSON(filepath.Join(*output, "usage.json"), ledger); e != nil {
		return fail(e)
	}
	if err != nil {
		return fail(err)
	}
	if err = prreview.WriteJSON(filepath.Join(*output, "review.json"), review); err != nil {
		return fail(err)
	}
	if err = os.WriteFile(filepath.Join(*output, "review.md"), []byte(prreview.Markdown(review, input)), 0600); err != nil {
		return fail(err)
	}
	fmt.Printf("Validated findings: %d; calls: %d; reserved: $%.8f; peak usage estimate: $%.8f\n", len(review.Findings), ledger.Calls, ledger.Reserved, ledger.EstimatedPeak)
	return 0
}
