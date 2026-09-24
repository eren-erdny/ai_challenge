package gitmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxJobs = 16
const retainedSamples = 32

type ScheduleInput struct {
	ID              string `json:"id" jsonschema:"Unique schedule ID: letters, digits, hyphen or underscore, up to 64 characters. Repeating identical creation is idempotent."`
	DelaySeconds    int    `json:"delay_seconds" jsonschema:"Delay before first execution, 1 to 604800 seconds"`
	IntervalSeconds int    `json:"interval_seconds,omitempty" jsonschema:"Repeat interval: 10 to 604800 seconds; 0 means execute once"`
}
type jobID struct {
	ID string `json:"id" jsonschema:"Schedule ID returned by git_summary_schedule"`
}
type Snapshot struct {
	Branch           string `json:"branch"`
	Head             string `json:"head"`
	ChangedEntries   int    `json:"changed_entries"`
	UntrackedEntries int    `json:"untracked_entries"`
	Conflicts        int    `json:"conflicts"`
}
type Sample struct {
	At       time.Time `json:"at"`
	Snapshot *Snapshot `json:"snapshot,omitempty"`
	Error    string    `json:"error,omitempty"`
}
type Job struct {
	ScheduleInput
	Active   bool      `json:"active"`
	NextRun  time.Time `json:"next_run"`
	Runs     int       `json:"runs"`
	Failures int       `json:"failures"`
	Samples  []Sample  `json:"samples"`
}
type scheduleDocument struct {
	Version    int    `json:"version"`
	Repository string `json:"repository"`
	Jobs       []Job  `json:"jobs"`
}

// Report aggregates the retained window; Runs/Failures are lifetime counters.
type Report struct {
	ID              string    `json:"id"`
	Active          bool      `json:"active"`
	NextRun         time.Time `json:"next_run"`
	IntervalSeconds int       `json:"interval_seconds"`
	Runs            int       `json:"runs"`
	Failures        int       `json:"failures"`
	RetainedSamples int       `json:"retained_samples"`
	From            time.Time `json:"from"`
	To              time.Time `json:"to"`
	DirtySamples    int       `json:"dirty_samples"`
	HeadChanges     int       `json:"observed_head_changes"`
	MaxConflicts    int       `json:"max_conflicts"`
	Latest          *Sample   `json:"latest,omitempty"`
	Summary         string    `json:"summary"`
}

type Scheduler struct {
	mu      sync.Mutex
	doc     scheduleDocument
	path    string
	lock    *os.File
	collect func(context.Context) (Snapshot, error)
	emit    func(Report)
}

var scheduleID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func validateSchedule(in ScheduleInput) error {
	if !scheduleID.MatchString(in.ID) {
		return errors.New("invalid schedule ID")
	}
	if in.DelaySeconds < 1 || in.DelaySeconds > 604800 {
		return errors.New("delay_seconds must be between 1 and 604800")
	}
	if in.IntervalSeconds != 0 && (in.IntervalSeconds < 10 || in.IntervalSeconds > 604800) {
		return errors.New("interval_seconds must be 0 or between 10 and 604800")
	}
	return nil
}

// OpenScheduler loads durable jobs and takes a process-wide OS file lock.
// dataDir is separate from the monitored clone so recording a sample cannot dirty it.
func OpenScheduler(repo, dataDir string, emit func(Report)) (*Scheduler, error) {
	root, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if insideRepository(root, dir) {
		return nil, errors.New("scheduler data directory must be outside the monitored repository")
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	if insideRepository(root, dir) {
		return nil, errors.New("scheduler data directory must be outside the monitored repository")
	}
	f, err := os.OpenFile(filepath.Join(dir, "scheduler.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockSchedulerFile(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("scheduler directory is already in use or cannot be locked: %w", err)
	}
	s := &Scheduler{path: filepath.Join(dir, "schedules.json"), lock: f, emit: emit, doc: scheduleDocument{Version: 1, Repository: root, Jobs: []Job{}}}
	s.collect = func(ctx context.Context) (Snapshot, error) { return collectSnapshot(ctx, root) }
	if err = s.load(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func insideRepository(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

// Close is called only after Run and all request handlers have stopped.
func (s *Scheduler) Close() error { return s.lock.Close() }

func (s *Scheduler) load() error {
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if err != nil {
		return err
	}
	if len(b) > 2<<20 {
		return errors.New("schedule JSON exceeds 2 MiB")
	}
	var doc scheduleDocument
	if err = json.Unmarshal(b, &doc); err != nil {
		return fmt.Errorf("invalid schedule JSON (not modified): %w", err)
	}
	if doc.Version != 1 || doc.Repository != s.doc.Repository || len(doc.Jobs) > maxJobs {
		return errors.New("schedule version, repository or job count mismatch; file not modified")
	}
	seen := map[string]bool{}
	for _, j := range doc.Jobs {
		if validateSchedule(j.ScheduleInput) != nil || seen[j.ID] || j.NextRun.IsZero() || j.Runs < 0 || j.Failures < 0 || j.Failures > j.Runs || len(j.Samples) > retainedSamples || len(j.Samples) > j.Runs {
			return errors.New("invalid persisted schedule; file not modified")
		}
		seen[j.ID] = true
		for _, sample := range j.Samples {
			if sample.At.IsZero() || (sample.Snapshot == nil) == (sample.Error == "") {
				return errors.New("invalid persisted sample; file not modified")
			}
		}
	}
	s.doc = doc
	return nil
}

func (s *Scheduler) save(doc scheduleDocument) error {
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if len(b) > 2<<20 {
		return errors.New("schedule state exceeds 2 MiB")
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".schedule-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(b); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), s.path); err != nil {
		return err
	}
	s.doc = doc
	return nil
}

func report(j Job) Report {
	r := Report{ID: j.ID, Active: j.Active, NextRun: j.NextRun, IntervalSeconds: j.IntervalSeconds, Runs: j.Runs, Failures: j.Failures, RetainedSamples: len(j.Samples)}
	var previous *Snapshot
	for _, sample := range j.Samples {
		if r.From.IsZero() {
			r.From = sample.At
		}
		r.To = sample.At
		if snap := sample.Snapshot; snap != nil {
			if snap.ChangedEntries+snap.UntrackedEntries > 0 {
				r.DirtySamples++
			}
			if snap.Conflicts > r.MaxConflicts {
				r.MaxConflicts = snap.Conflicts
			}
			if previous != nil && previous.Head != snap.Head {
				r.HeadChanges++
			}
			previous = snap
		}
	}
	if len(j.Samples) > 0 {
		last := j.Samples[len(j.Samples)-1]
		if last.Snapshot != nil {
			copy := *last.Snapshot
			last.Snapshot = &copy
		}
		r.Latest = &last
	}
	r.Summary = fmt.Sprintf("%s: runs=%d, failures=%d; retained=%d, dirty_samples=%d, observed_HEAD_changes=%d, max_conflicts=%d", r.ID, r.Runs, r.Failures, r.RetainedSamples, r.DirtySamples, r.HeadChanges, r.MaxConflicts)
	if r.Latest != nil && r.Latest.Snapshot != nil {
		snap := r.Latest.Snapshot
		r.Summary += fmt.Sprintf("; latest branch=%s, changed=%d, untracked=%d, conflicts=%d", snap.Branch, snap.ChangedEntries, snap.UntrackedEntries, snap.Conflicts)
	}
	if r.Latest != nil && r.Latest.Error != "" {
		r.Summary += "; latest execution failed"
	}
	return r
}

func (s *Scheduler) Create(in ScheduleInput, now time.Time) (Report, error) {
	if err := validateSchedule(in); err != nil {
		return Report{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.doc.Jobs {
		if j.ID == in.ID {
			if j.ScheduleInput == in {
				return report(j), nil
			}
			return Report{}, errors.New("ID already exists with different settings; choose another ID")
		}
	}
	if len(s.doc.Jobs) >= maxJobs {
		return Report{}, errors.New("maximum 16 stored schedules reached")
	}
	j := Job{ScheduleInput: in, Active: true, NextRun: now.UTC().Add(time.Duration(in.DelaySeconds) * time.Second), Samples: []Sample{}}
	doc := s.doc
	doc.Jobs = append(append([]Job(nil), doc.Jobs...), j)
	if err := s.save(doc); err != nil {
		return Report{}, err
	}
	return report(j), nil
}
func (s *Scheduler) Reports() []Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Report{}
	for _, j := range s.doc.Jobs {
		out = append(out, report(j))
	}
	return out
}
func (s *Scheduler) Get(id string) (Report, error) {
	for _, r := range s.Reports() {
		if r.ID == id {
			return r, nil
		}
	}
	return Report{}, errors.New("unknown schedule ID")
}
func (s *Scheduler) Cancel(id string) (Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.doc
	doc.Jobs = append([]Job(nil), doc.Jobs...)
	for i := range doc.Jobs {
		if doc.Jobs[i].ID == id {
			doc.Jobs[i].Active = false
			if err := s.save(doc); err != nil {
				return Report{}, err
			}
			return report(doc.Jobs[i]), nil
		}
	}
	return Report{}, errors.New("unknown schedule ID")
}

// Run resumes overdue schedules once, then advances from the completion time.
// It never fabricates samples for missed intervals. Persistence failure stops the worker.
func (s *Scheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := s.runDue(ctx, time.Now().UTC()); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
func (s *Scheduler) runDue(ctx context.Context, now time.Time) error {
	for _, r := range s.Reports() {
		if ctx.Err() != nil {
			return nil
		}
		if !r.Active || r.NextRun.After(now) {
			continue
		}
		completed, err := s.execute(ctx, r.ID, now)
		if err != nil {
			return err
		}
		if completed != nil && s.emit != nil {
			s.emit(*completed)
		}
	}
	return nil
}
func (s *Scheduler) execute(ctx context.Context, id string, now time.Time) (*Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.doc
	doc.Jobs = append([]Job(nil), doc.Jobs...)
	for i := range doc.Jobs {
		j := &doc.Jobs[i]
		if j.ID != id || !j.Active || j.NextRun.After(now) {
			continue
		}
		collectCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		snapshot, err := s.collect(collectCtx)
		cancel()
		if ctx.Err() != nil {
			return nil, nil
		}
		sample := Sample{At: now}
		if err != nil {
			message := err.Error()
			if len(message) > 1024 {
				message = message[:1024]
			}
			sample.Error = message
			j.Failures++
		} else {
			sample.Snapshot = &snapshot
		}
		j.Runs++
		j.Samples = append(append([]Sample(nil), j.Samples...), sample)
		if len(j.Samples) > retainedSamples {
			j.Samples = j.Samples[len(j.Samples)-retainedSamples:]
		}
		if j.IntervalSeconds == 0 {
			j.Active = false
		} else {
			base := time.Now().UTC()
			if now.After(base) {
				base = now
			}
			j.NextRun = base.Add(time.Duration(j.IntervalSeconds) * time.Second)
		}
		if err := s.save(doc); err != nil {
			return nil, fmt.Errorf("persist scheduled result: %w", err)
		}
		r := report(*j)
		return &r, nil
	}
	return nil, nil
}

func collectSnapshot(ctx context.Context, root string) (Snapshot, error) {
	var snap Snapshot
	status, err := run(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
	if err != nil {
		return snap, err
	}
	if status.Truncated {
		return snap, errors.New("Git status exceeds output limit; snapshot not counted as success")
	}
	entries := strings.Split(status.Text, "\x00")
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		if entry == "" {
			continue
		}
		if len(entry) < 3 {
			return snap, errors.New("invalid Git status output")
		}
		xy := entry[:2]
		if xy == "??" {
			snap.UntrackedEntries++
		} else {
			snap.ChangedEntries++
		}
		if strings.Contains(xy, "U") || xy == "AA" || xy == "DD" {
			snap.Conflicts++
		}
		if strings.ContainsAny(xy, "RC") {
			i++
		}
	}
	branch, err := run(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err == nil {
		snap.Branch = strings.TrimSpace(branch.Text)
	} else {
		snap.Branch = "(detached)"
	}
	head, err := run(ctx, root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		if snap.Branch == "(detached)" {
			return snap, err
		}
		snap.Head = "(unborn)"
	} else {
		snap.Head = strings.TrimSpace(head.Text)
	}
	if ctx.Err() != nil {
		return snap, ctx.Err()
	}
	return snap, nil
}

func (s *Scheduler) Register(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "git_summary_schedule", Description: "Persist a delayed or periodic Git summary job on the server. Runs without a connected client or LLM; emits summaries to server logs. Does not push notifications into chat."}, func(_ context.Context, _ *mcp.CallToolRequest, in ScheduleInput) (*mcp.CallToolResult, Report, error) {
		r, e := s.Create(in, time.Now().UTC())
		return nil, r, e
	})
	mcp.AddTool(server, &mcp.Tool{Name: "git_summary_list", Description: "List persisted schedules and aggregated Git results."}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, []Report, error) {
		return nil, s.Reports(), nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "git_summary_get", Description: "Get aggregate over retained samples, lifetime counters and latest result for a schedule. Does not trigger execution."}, func(_ context.Context, _ *mcp.CallToolRequest, in jobID) (*mcp.CallToolResult, Report, error) {
		r, e := s.Get(in.ID)
		return nil, r, e
	})
	mcp.AddTool(server, &mcp.Tool{Name: "git_summary_cancel", Description: "Stop future runs of a schedule, preserving its aggregated results. Does not reactivate an existing ID."}, func(_ context.Context, _ *mcp.CallToolRequest, in jobID) (*mcp.CallToolResult, Report, error) {
		r, e := s.Cancel(in.ID)
		return nil, r, e
	})
}
