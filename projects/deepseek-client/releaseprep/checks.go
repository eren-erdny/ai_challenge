package releaseprep

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Snapshot executes no Git hooks and checks the exact selected commit, not dirty files.
func Snapshot(ctx context.Context, repo, head, output string) error {
	data, err := git(ctx, repo, 32<<20, "archive", "--format=tar", head, "projects/deepseek-client", "scripts/ai-review", "examples/file-assistant/result", "examples/support-assistant")
	if err != nil {
		return err
	}
	if err = os.Mkdir(output, 0700); err != nil {
		return err
	}
	r := tar.NewReader(bytes.NewReader(data))
	count := 0
	total := int64(0)
	for {
		h, e := r.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := strings.TrimSuffix(h.Name, "/")
		if !safePath(name) {
			return errors.New("unsafe archive path")
		}
		allowed := false
		for _, prefix := range []string{"projects", "scripts", "examples"} {
			if name == prefix || strings.HasPrefix(name, prefix+"/") {
				allowed = true
			}
		}
		if !allowed {
			return errors.New("unexpected archive scope")
		}
		if h.Typeflag != tar.TypeDir && h.Typeflag != tar.TypeReg {
			return errors.New("release snapshots reject symlinks and special files")
		}
		dst := filepath.Join(output, filepath.FromSlash(name))
		if h.Typeflag == tar.TypeDir {
			if e = os.MkdirAll(dst, 0700); e != nil {
				return e
			}
			continue
		}
		count++
		total += h.Size
		if count > 1000 || h.Size > 2<<20 || total > 24<<20 {
			return errors.New("snapshot exceeds bounds")
		}
		if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
			return e
		}
		b, e := io.ReadAll(io.LimitReader(r, h.Size+1))
		if e != nil || int64(len(b)) != h.Size {
			return errors.New("invalid archive file length")
		}
		if e = os.WriteFile(dst, b, 0600); e != nil {
			return e
		}
	}
	return nil
}

func RunChecks(ctx context.Context, snapshot, output, node, python string) ([]Check, error) {
	jobs := []struct {
		name, dir string
		command   []string
	}{
		{"go-tests", "projects/deepseek-client", []string{"go", "test", "./..."}},
		{"go-vet", "projects/deepseek-client", []string{"go", "vet", "./..."}},
		{"go-build", "projects/deepseek-client", []string{"go", "build", "-o", "release-check.exe", "."}},
		{"node-tests", "", []string{node, "--test", "projects/deepseek-client/fileassistant/fixture/test/export.test.js", "examples/file-assistant/result/test/export.test.js"}},
		{"publisher-tests", "", []string{python, "-m", "unittest", "discover", "-s", "scripts/ai-review", "-p", "test_*.py", "-v"}},
	}
	var result []Check
	for _, job := range jobs {
		start := time.Now()
		callCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		check := Check{Name: job.name, Command: job.command, Status: "failed", ExitCode: -1, Log: job.name + ".log"}
		cmd := exec.CommandContext(callCtx, job.command[0], job.command[1:]...)
		cmd.Dir = filepath.Join(snapshot, filepath.FromSlash(job.dir))
		cmd.Env = append(cleanEnv(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly -buildvcs=false")
		log := &limited{max: 192 << 10}
		cmd.Stdout = log
		cmd.Stderr = log
		err := cmd.Run()
		check.Seconds = time.Since(start).Seconds()
		if err == nil {
			check.ExitCode = 0
			check.Status = "passed"
		} else if cmd.ProcessState != nil {
			check.ExitCode = cmd.ProcessState.ExitCode()
		}
		if callCtx.Err() != nil {
			check.Status = "timeout"
		}
		if log.overflow {
			check.Status = "log_overflow"
		}
		cancel()
		text := log.String()
		if err != nil {
			text += "\nApplication: check failed or unavailable; no AI verdict can override this.\n"
		}
		if e := os.WriteFile(filepath.Join(output, check.Log), []byte(text), 0600); e != nil {
			return result, e
		}
		result = append(result, check)
		if e := WriteJSON(filepath.Join(output, "checks.json"), result); e != nil {
			return result, e
		}
	}
	return result, nil
}

func ChecksPassed(checks []Check) bool {
	if len(checks) != 5 {
		return false
	}
	for _, c := range checks {
		if c.Status != "passed" || c.ExitCode != 0 {
			return false
		}
	}
	return true
}
