package prreview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type ChangedLine struct {
	Side string `json:"side"`
	Line int    `json:"line"`
	Text string `json:"text"`
}
type File struct {
	Path   string        `json:"path"`
	Status string        `json:"status"`
	Patch  string        `json:"patch"`
	Lines  []ChangedLine `json:"changed_lines"`
}
type Chunk struct {
	ID    string  `json:"id"`
	Path  string  `json:"path"`
	Kind  string  `json:"kind"`
	Start int     `json:"start_line"`
	End   int     `json:"end_line"`
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}
type Input struct {
	Base         string   `json:"base_sha"`
	Head         string   `json:"head_sha"`
	MergeBase    string   `json:"merge_base_sha"`
	Scope        string   `json:"scope"`
	Files        []File   `json:"files"`
	Skipped      []string `json:"skipped"`
	Context      []Chunk  `json:"retrieved_context"`
	CorpusFiles  int      `json:"corpus_files"`
	CorpusChunks int      `json:"corpus_chunks"`
}
type boundedOutput struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > b.limit-b.Len() {
		b.overflow = true
		p = p[:max(0, b.limit-b.Len())]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
func git(ctx context.Context, repo string, limit int, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-pager", "--no-optional-locks", "--literal-pathspecs", "--no-replace-objects", "-c", "core.fsmonitor=false", "-c", "color.ui=false", "-C", repo}, args...)...)
	for _, item := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(item), "GIT_") {
			cmd.Env = append(cmd.Env, item)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	output := &boundedOutput{limit: limit}
	cmd.Stdout = output
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git read failed: %w", err)
	}
	if output.overflow {
		return nil, errors.New("Git output exceeds review limit; refusing partial review")
	}
	return output.Bytes(), nil
}

var fullSHA = regexp.MustCompile("^[a-fA-F0-9]{40}([a-fA-F0-9]{24})?$")
var hunkHeader = regexp.MustCompile("^@@ -([0-9]+)(?:,[0-9]+)? [+]([0-9]+)(?:,[0-9]+)? @@")

func safePath(p string) bool {
	if p == "" || !utf8.ValidString(p) || path.IsAbs(p) || path.Clean(p) != p || strings.ContainsAny(p, "\\:\x00") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || strings.HasPrefix(part, ".") {
			return false
		}
		for _, r := range part {
			if unicode.IsControl(r) {
				return false
			}
		}
	}
	return true
}
func allowed(p string) bool {
	if !safePath(p) {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		switch part {
		case "node_modules", "vendor", "conversations", "memory", "artifacts", "data", "models", "examples":
			return false
		}
	}
	switch strings.ToLower(path.Ext(p)) {
	case ".go", ".py", ".js", ".ts", ".tsx", ".rs", ".java", ".c", ".cpp", ".h", ".md", ".markdown":
		return true
	}
	return false
}
func changedLines(patch string) ([]ChangedLine, error) {
	result := []ChangedLine{}
	old, newLine := 0, 0
	inHunk := false
	for _, line := range strings.Split(patch, "\n") {
		if match := hunkHeader.FindStringSubmatch(line); match != nil {
			old, _ = strconv.Atoi(match[1])
			newLine, _ = strconv.Atoi(match[2])
			inHunk = true
			continue
		}
		if !inHunk || line == "" {
			continue
		}
		switch line[0] {
		case '+':
			result = append(result, ChangedLine{"RIGHT", newLine, line[1:]})
			newLine++
		case '-':
			result = append(result, ChangedLine{"LEFT", old, line[1:]})
			old++
		case ' ':
			old++
			newLine++
		case '\\':
		default:
			return nil, errors.New("unrecognized diff hunk")
		}
	}
	return result, nil
}

// Collect reads immutable Git objects. Neither PR files nor hooks are executed.
func Collect(ctx context.Context, repo, base, head, scope string) (Input, []Chunk, error) {
	in := Input{Base: base, Head: head, Scope: scope, Files: []File{}, Skipped: []string{}, Context: []Chunk{}}
	if !fullSHA.MatchString(base) || !fullSHA.MatchString(head) || !safePath(scope) {
		return in, nil, errors.New("full commit SHAs and a clean relative scope are required")
	}
	merge, err := git(ctx, repo, 256, "merge-base", base, head)
	if err != nil {
		return in, nil, err
	}
	in.MergeBase = strings.TrimSpace(string(merge))
	names, err := git(ctx, repo, 1<<20, "diff", "--name-status", "-z", "--no-renames", in.MergeBase, head, "--", scope)
	if err != nil {
		return in, nil, err
	}
	parts := strings.Split(strings.TrimSuffix(string(names), "\x00"), "\x00")
	total := 0
	if len(names) > 0 {
		if len(parts)%2 != 0 {
			return in, nil, errors.New("invalid changed-file list")
		}
		if len(parts)/2 > 40 {
			return in, nil, errors.New("more than 40 changed files; split the PR")
		}
		for i := 0; i < len(parts); i += 2 {
			status, p := parts[i], parts[i+1]
			if !allowed(p) {
				in.Skipped = append(in.Skipped, p)
				continue
			}
			patch, err := git(ctx, repo, 60000, "diff", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames", "--unified=4", in.MergeBase, head, "--", p)
			if err != nil {
				return in, nil, err
			}
			lines, err := changedLines(string(patch))
			if err != nil {
				return in, nil, err
			}
			if len(lines) == 0 {
				in.Skipped = append(in.Skipped, p+" (binary, mode-only or no text changes)")
				continue
			}
			total += len(patch)
			if total > 80000 {
				return in, nil, errors.New("diff exceeds 80000 bytes; refusing a truncated review")
			}
			in.Files = append(in.Files, File{p, status, string(patch), lines})
		}
	}
	tree, err := git(ctx, repo, 1<<20, "ls-tree", "-r", "-z", base, "--", scope)
	if err != nil {
		return in, nil, err
	}
	chunks := []Chunk{}
	corpusBytes := 0
	for _, record := range strings.Split(strings.TrimSuffix(string(tree), "\x00"), "\x00") {
		fields := strings.SplitN(record, "\t", 2)
		if len(fields) != 2 {
			continue
		}
		meta, p := strings.Fields(fields[0]), fields[1]
		if len(meta) != 3 || meta[1] != "blob" || (meta[0] != "100644" && meta[0] != "100755") || !allowed(p) {
			continue
		}
		sizeRaw, err := git(ctx, repo, 64, "cat-file", "-s", meta[2])
		if err != nil {
			return in, nil, err
		}
		size, _ := strconv.Atoi(strings.TrimSpace(string(sizeRaw)))
		if size > 128<<10 {
			continue
		}
		blob, err := git(ctx, repo, 128<<10, "cat-file", "blob", meta[2])
		if err != nil {
			return in, nil, err
		}
		if !utf8.Valid(blob) || bytes.IndexByte(blob, 0) >= 0 {
			continue
		}
		corpusBytes += len(blob)
		in.CorpusFiles++
		if corpusBytes > 8<<20 || in.CorpusFiles > 400 {
			return in, nil, errors.New("base corpus exceeds bounds; narrow the scope")
		}
		chunks = append(chunks, chunkFile(p, string(blob))...)
	}
	in.CorpusChunks = len(chunks)
	if len(in.Files) > 0 {
		var query strings.Builder
		for _, f := range in.Files {
			query.WriteString(f.Path + " ")
			for _, line := range f.Lines {
				query.WriteString(line.Text + " ")
			}
		}
		in.Context = Retrieve(chunks, query.String())
	}
	return in, chunks, nil
}
