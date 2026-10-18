// Package releaseprep prepares a reviewable release bundle from immutable Git objects.
package releaseprep

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/eren-erdny/ai_challenge/projects/deepseek-client/prreview"
)

type Commit struct {
	SHA     string   `json:"sha"`
	Subject string   `json:"subject"`
	Files   []string `json:"files"`
}
type ChangedFile struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}
type Check struct {
	Name     string   `json:"name"`
	Command  []string `json:"command"`
	Status   string   `json:"status"`
	ExitCode int      `json:"exit_code"`
	Seconds  float64  `json:"seconds"`
	Log      string   `json:"log"`
}
type Input struct {
	Version      string           `json:"version"`
	Base         string           `json:"base_sha"`
	Head         string           `json:"head_sha"`
	Commits      []Commit         `json:"commits"`
	Files        []ChangedFile    `json:"changed_files"`
	Evidence     []prreview.Chunk `json:"selected_head_evidence"`
	Checks       []Check          `json:"checks"`
	CorpusFiles  int              `json:"corpus_files"`
	CorpusChunks int              `json:"corpus_chunks"`
	Scope        string           `json:"scope"`
}

var refPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,150}$`)
var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type limited struct {
	bytes.Buffer
	max      int
	overflow bool
}

func (b *limited) Write(p []byte) (int, error) {
	n := len(p)
	if n > b.max-b.Len() {
		b.overflow = true
		p = p[:max(0, b.max-b.Len())]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
func cleanEnv() []string {
	allow := map[string]bool{"PATH": true, "PATHEXT": true, "SYSTEMROOT": true, "WINDIR": true, "TEMP": true, "TMP": true, "USERPROFILE": true, "HOME": true, "LOCALAPPDATA": true, "APPDATA": true, "COMSPEC": true, "GOCACHE": true, "GOPATH": true, "GOMODCACHE": true}
	var env []string
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if allow[strings.ToUpper(key)] {
			env = append(env, item)
		}
	}
	return env
}
func git(ctx context.Context, repo string, limit int, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-pager", "--no-replace-objects", "--literal-pathspecs", "-c", "core.fsmonitor=false", "-c", "color.ui=false", "-C", repo}, args...)...)
	cmd.Env = append(cleanEnv(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	out := &limited{max: limit}
	cmd.Stdout = out
	cmd.Stderr = &limited{max: 4096}
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("Git read failed: %w", err)
	}
	if out.overflow {
		return nil, errors.New("Git data exceeds release bounds; no partial bundle is accepted")
	}
	return out.Bytes(), nil
}
func resolve(ctx context.Context, repo, ref string) (string, error) {
	if !refPattern.MatchString(ref) || strings.Contains(ref, "..") {
		return "", errors.New("invalid Git reference")
	}
	b, err := git(ctx, repo, 128, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
func safePath(p string) bool {
	return p != "" && utf8.ValidString(p) && path.Clean(p) == p && !path.IsAbs(p) && !strings.ContainsAny(p, "\\:\x00\r\n") && !strings.HasPrefix(p, "../")
}
func chunks(p, text string) []prreview.Chunk {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	kind := "code"
	if strings.HasSuffix(p, ".md") {
		kind = "documentation"
	}
	var result []prreview.Chunk
	for start := 0; start < len(lines); start += 18 {
		end := min(start+24, len(lines))
		s := strings.Join(lines[start:end], "\n")
		if len(s) > 4500 || strings.TrimSpace(s) == "" {
			continue
		}
		h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", p, start, s)))
		result = append(result, prreview.Chunk{ID: fmt.Sprintf("%x", h[:12]), Path: p, Kind: kind, Start: start + 1, End: end, Text: s})
		if end == len(lines) {
			break
		}
	}
	return result
}
func corpusPath(p string) bool {
	if !strings.HasPrefix(p, "projects/deepseek-client/") {
		return false
	}
	if strings.Contains(p, "/testdata/") || strings.Contains(p, "/fixture/") {
		return false
	}
	return strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".go") || strings.HasSuffix(p, ".html")
}
func Collect(ctx context.Context, repo, baseRef, headRef, version string) (Input, []byte, error) {
	in := Input{Version: version, Scope: "All Git file changes and non-merge commits; AI evidence is a bounded selection from changed DeepSeek Client source/docs at head. Full diff stays local."}
	if !versionPattern.MatchString(version) {
		return in, nil, errors.New("invalid release version label")
	}
	var err error
	in.Base, err = resolve(ctx, repo, baseRef)
	if err != nil {
		return in, nil, err
	}
	in.Head, err = resolve(ctx, repo, headRef)
	if err != nil {
		return in, nil, err
	}
	if in.Base == in.Head {
		return in, nil, errors.New("release range has no changes")
	}
	if _, err = git(ctx, repo, 128, "merge-base", "--is-ancestor", in.Base, in.Head); err != nil {
		return in, nil, errors.New("base must be an ancestor of selected head")
	}
	data, err := git(ctx, repo, 256<<10, "diff", "--name-status", "--no-renames", "-z", in.Base, in.Head)
	if err != nil {
		return in, nil, err
	}
	parts := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
	if len(parts)%2 != 0 {
		return in, nil, errors.New("invalid Git file listing")
	}
	for i := 0; i < len(parts); i += 2 {
		if !safePath(parts[i+1]) {
			return in, nil, errors.New("unsupported changed path")
		}
		in.Files = append(in.Files, ChangedFile{parts[i], parts[i+1]})
	}
	if len(in.Files) == 0 || len(in.Files) > 256 {
		return in, nil, errors.New("release range must contain 1 to 256 changed files")
	}
	patch, err := git(ctx, repo, 4<<20, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--unified=3", in.Base, in.Head)
	if err != nil {
		return in, nil, err
	}
	log, err := git(ctx, repo, 64<<10, "log", "--no-merges", "--format=%H %s", in.Base+".."+in.Head)
	if err != nil {
		return in, nil, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(log)), "\n") {
		sha, subject, ok := strings.Cut(line, " ")
		if !ok {
			return in, nil, errors.New("invalid commit listing")
		}
		names, e := git(ctx, repo, 64<<10, "diff-tree", "--no-commit-id", "--name-only", "--no-renames", "-r", sha)
		if e != nil {
			return in, nil, e
		}
		in.Commits = append(in.Commits, Commit{sha, subject, strings.Fields(string(names))})
	}
	if len(in.Commits) > 50 {
		return in, nil, errors.New("at most 50 non-merge commits per release")
	}
	var corpus []prreview.Chunk
	total := 0
	for _, f := range in.Files {
		if f.Status == "D" || !corpusPath(f.Path) {
			continue
		}
		size, err := git(ctx, repo, 64, "cat-file", "-s", in.Head+":"+f.Path)
		if err != nil {
			return in, nil, err
		}
		var n int
		if _, err = fmt.Sscanf(string(size), "%d", &n); err != nil || n > 128<<10 {
			continue
		}
		b, err := git(ctx, repo, 128<<10, "show", in.Head+":"+f.Path)
		if err != nil {
			return in, nil, err
		}
		if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
			continue
		}
		total += len(b)
		if total > 4<<20 {
			return in, nil, errors.New("release evidence corpus exceeds 4 MiB")
		}
		in.CorpusFiles++
		corpus = append(corpus, chunks(f.Path, string(b))...)
	}
	in.CorpusChunks = len(corpus)
	seen := map[string]bool{}
	bytesSelected := 0
	// Per-commit retrieval gives independent features a chance to supply evidence.
	for i := len(in.Commits) - 1; i >= 0; i-- {
		c := in.Commits[i]
		paths := map[string]bool{}
		for _, p := range c.Files {
			paths[p] = true
		}
		var candidates []prreview.Chunk
		for _, chunk := range corpus {
			if paths[chunk.Path] {
				candidates = append(candidates, chunk)
			}
		}
		docs, code := 0, 0
		for _, chunk := range prreview.Retrieve(candidates, c.Subject+" release features commands limitations setup "+strings.Join(c.Files, " ")) {
			if seen[chunk.ID] || len(in.Evidence) >= 16 || bytesSelected+len(chunk.Text) > 30000 {
				continue
			}
			if chunk.Kind == "documentation" {
				if docs >= 2 {
					continue
				}
				docs++
			} else {
				if code >= 1 {
					continue
				}
				code++
			}
			seen[chunk.ID] = true
			bytesSelected += len(chunk.Text)
			in.Evidence = append(in.Evidence, chunk)
		}
	}
	if len(in.Evidence) == 0 {
		return in, nil, errors.New("no eligible release evidence")
	}
	return in, patch, nil
}
func WriteJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}
func EvidenceJSON(in Input) string {
	var results []map[string]string
	for _, c := range in.Evidence {
		results = append(results, map[string]string{"source": c.Path, "section": fmt.Sprintf("head lines %d-%d", c.Start, c.End), "chunk_id": c.ID, "text": c.Text})
	}
	b, _ := json.Marshal(map[string]any{"results": results})
	return string(b)
}
func ChangedMarkdown(in Input) string {
	files := append([]ChangedFile(nil), in.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	var b strings.Builder
	fmt.Fprintf(&b, "# Changed files: %s\n\n%s → %s\n\n", in.Version, in.Base, in.Head)
	for _, f := range files {
		fmt.Fprintf(&b, "- %s `%s`\n", f.Status, f.Path)
	}
	return b.String()
}
func Root(ctx context.Context, repo string) (string, error) {
	b, e := git(ctx, repo, 4096, "rev-parse", "--show-toplevel")
	if e != nil {
		return "", e
	}
	return filepath.Clean(strings.TrimSpace(string(b))), nil
}
