package history

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Prune removes only validated, inactive conversation files older than the cutoff.
// ModTime is the last successful save (or creation for an empty conversation).
func (s *JSON) Prune(ctx context.Context, activeID string, days int, now time.Time) (int, error) {
	if days < 0 || days > 106751 {
		return 0, errors.New("invalid retention days")
	}
	if days == 0 {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !validID.MatchString(activeID) {
		return 0, errors.New("active conversation ID is required")
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return 0, err
	}
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
	removed := 0
	var failures []error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return removed, errors.Join(append(failures, err)...)
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if id == activeID || !validID.MatchString(id) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !info.Mode().IsRegular() || !info.ModTime().Before(cutoff) {
			continue
		}
		// Foreign JSON, broken files, metadata and symlinks must never be deleted.
		if _, err := s.Load(ctx, id); err != nil {
			failures = append(failures, fmt.Errorf("skip %s: %w", id, err))
			continue
		}
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		if err := os.Remove(filepath.Join(s.Dir, entry.Name())); err != nil {
			failures = append(failures, fmt.Errorf("remove %s: %w", id, err))
			continue
		}
		removed++
	}
	return removed, errors.Join(failures...)
}
