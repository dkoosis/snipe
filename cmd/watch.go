package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watchDrainTimeout bounds the best-effort final reindex performed when the
// watch loop is cancelled, so shutdown can't hang.
const watchDrainTimeout = 5 * time.Second

var (
	watchDebounce int
	watchVerbose  bool
)

// WatchEvent represents a reindex event for JSON output.
type WatchEvent struct {
	Event     string   `json:"event"`
	Files     []string `json:"files,omitempty"`
	Ms        int64    `json:"ms,omitempty"`
	Error     string   `json:"error,omitempty"`
	Timestamp string   `json:"timestamp"`
}

func runWatch() error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer watcher.Close()

	// Add directories recursively
	if err := addWatchDirs(watcher, dir); err != nil {
		return fmt.Errorf("add watch directories: %w", err)
	}

	emitEvent(WatchEvent{Event: "started", Timestamp: time.Now().Format(time.RFC3339)})

	// Debounce timer
	debounceTimer := time.NewTimer(0)
	<-debounceTimer.C // Drain initial timer

	var pendingFiles []string
	debounceDuration := time.Duration(watchDebounce) * time.Millisecond

	reindexDone := make(chan reindexResultT, 1)
	indexing := false

	startReindex := func(files []string) {
		indexing = true
		emitEvent(WatchEvent{
			Event:     "reindex_started",
			Files:     files,
			Timestamp: time.Now().Format(time.RFC3339),
		})
		go func() {
			start := time.Now()
			err := runReindex(GetContext(), dir)
			reindexDone <- reindexResultT{files: files, elapsed: time.Since(start), err: err}
		}()
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			// Pick up directories created after startup (fsnotify only watches
			// dirs registered at Add() time on macOS/Linux).
			if event.Op&fsnotify.Create != 0 {
				if fi, statErr := os.Stat(event.Name); statErr == nil && fi.IsDir() {
					_ = addWatchDirs(watcher, event.Name)
				}
			}

			// Only watch .go files
			if !strings.HasSuffix(event.Name, ".go") {
				continue
			}

			// Skip temporary files
			if strings.Contains(event.Name, "~") || strings.HasPrefix(filepath.Base(event.Name), ".") {
				continue
			}

			// Track changed file
			relPath, _ := filepath.Rel(dir, event.Name)
			if relPath == "" {
				relPath = event.Name
			}

			// Avoid duplicates
			found := false
			for _, f := range pendingFiles {
				if f == relPath {
					found = true
					break
				}
			}
			if !found {
				pendingFiles = append(pendingFiles, relPath)
			}

			// Reset debounce timer (stop+drain before reset to avoid race)
			if !debounceTimer.Stop() {
				select {
				case <-debounceTimer.C:
				default:
				}
			}
			debounceTimer.Reset(debounceDuration)

			if watchVerbose {
				emitEvent(WatchEvent{
					Event:     "change_detected",
					Files:     []string{relPath},
					Timestamp: time.Now().Format(time.RFC3339),
				})
			}

		case <-debounceTimer.C:
			if len(pendingFiles) == 0 || indexing {
				// If indexing, leave pendingFiles; reindexDone will re-arm.
				continue
			}

			files := pendingFiles
			pendingFiles = nil
			startReindex(files)

		case res := <-reindexDone:
			indexing = false
			// Suppress error emit when parent ctx is cancelled — the child
			// was killed by exec.CommandContext and the next iteration will
			// emit "stopped". Don't double-report as a real failure.
			if res.err != nil && GetContext().Err() == nil {
				emitEvent(WatchEvent{
					Event:     cmdKindError,
					Error:     res.err.Error(),
					Timestamp: time.Now().Format(time.RFC3339),
				})
			} else if res.err == nil {
				emitEvent(WatchEvent{
					Event:     "reindexed",
					Files:     res.files,
					Ms:        res.elapsed.Milliseconds(),
					Timestamp: time.Now().Format(time.RFC3339),
				})
			}
			// Files arrived during reindex — re-arm debounce to coalesce.
			if len(pendingFiles) > 0 {
				if !debounceTimer.Stop() {
					select {
					case <-debounceTimer.C:
					default:
					}
				}
				debounceTimer.Reset(debounceDuration)
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			emitEvent(WatchEvent{
				Event:     cmdKindError,
				Error:     err.Error(),
				Timestamp: time.Now().Format(time.RFC3339),
			})

		case <-GetContext().Done():
			// Best-effort final drain: a reindex finishing concurrently with
			// cancel (or files queued during one) would otherwise be dropped,
			// losing the last edit before Ctrl+C. The parent ctx is already
			// cancelled, so use a fresh bounded context for the drain.
			drainOnCancel(dir, indexing, pendingFiles, reindexDone)
			emitEvent(WatchEvent{
				Event:     "stopped",
				Timestamp: time.Now().Format(time.RFC3339),
			})
			return GetContext().Err()
		}
	}
}

// drainOnCancel performs a best-effort final reindex when the watch loop is
// cancelled, so files edited just before shutdown aren't silently dropped.
//
// If a reindex is already in flight (indexing), it waits briefly (bounded) for
// it to finish, folding any files it carried plus newly-pending files into the
// drain set, rather than starting an overlapping reindex. It then runs ONE last
// synchronous reindex with a short bounded context if anything remains pending.
//
// reindexFn is injectable for testing; nil falls back to the real runReindex.
func drainOnCancel(dir string, indexing bool, pending []string, reindexDone <-chan reindexResultT) {
	drainOnCancelWith(dir, indexing, pending, reindexDone, nil)
}

// reindexResultT carries a reindex goroutine's outcome on reindexDone. It is a
// package-level type (not loop-local) so the drainOnCancel seam can be
// unit-tested without reconstructing the watch loop.
type reindexResultT struct {
	files   []string
	elapsed time.Duration
	err     error
}

func drainOnCancelWith(
	dir string,
	indexing bool,
	pending []string,
	reindexDone <-chan reindexResultT,
	reindexFn func(context.Context, string) error,
) {
	if reindexFn == nil {
		reindexFn = runReindex
	}

	ctx, cancel := context.WithTimeout(context.Background(), watchDrainTimeout)
	defer cancel()

	// An in-flight reindex's subprocess was killed by the cancelled parent ctx,
	// but its goroutine still sends on reindexDone. Wait (bounded) for it so we
	// don't start an overlapping reindex; its files are already pending again.
	if indexing {
		select {
		case res := <-reindexDone:
			// The killed reindex didn't durably cover its files — fold them
			// back in so the final drain re-runs them.
			if res.err != nil {
				pending = append(pending, res.files...)
			}
		case <-ctx.Done():
			// Timed out waiting; fall through and attempt a drain anyway.
		}
	}

	if len(pending) == 0 {
		return
	}

	emitEvent(WatchEvent{
		Event:     "reindex_started",
		Files:     pending,
		Timestamp: time.Now().Format(time.RFC3339),
	})
	if err := reindexFn(ctx, dir); err != nil {
		if ctx.Err() == nil {
			emitEvent(WatchEvent{
				Event:     cmdKindError,
				Error:     err.Error(),
				Timestamp: time.Now().Format(time.RFC3339),
			})
		}
		return
	}
	emitEvent(WatchEvent{
		Event:     "reindexed",
		Files:     pending,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

// addWatchDirs recursively adds directories to the watcher.
func addWatchDirs(watcher *fsnotify.Watcher, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // Skip inaccessible directories gracefully
		}

		// Skip hidden directories and common non-source directories
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".") || base == "vendor" || base == "node_modules" || base == "testdata" {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			if err := watcher.Add(path); err != nil {
				// Don't fail on permission errors
				if watchVerbose {
					fmt.Fprintf(os.Stderr, "Warning: could not watch %s: %v\n", path, err)
				}
			}
		}

		return nil
	})
}

// runReindex runs 'snipe index' as a subprocess, bound to ctx.
// V1: Full reindex. V2 will add incremental support.
func runReindex(ctx context.Context, dir string) error {
	// Find the snipe binary
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	cmd := exec.CommandContext(ctx, exe, "index") // #nosec G204 -- exe from os.Executable(), trusted self-invocation
	cmd.Dir = dir
	cmd.Stdout = os.Stderr // Redirect index output to stderr
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// emitEvent outputs a JSON event to stdout.
func emitEvent(event WatchEvent) {
	data, _ := json.Marshal(event)
	fmt.Println(string(data))
}
