package watch

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

const defaultFileDebounce = 500 * time.Millisecond

// RunFileWatch watches a single file path via its parent directory (so
// creation is observable) and emits file.created/changed/removed.
// Initial check: an existing file emits file.created immediately.
// Same-type events within the debounce window (Config.StabilityThreshold,
// default 500ms) collapse to one — editors write in bursts.
// Blocks until ctx is cancelled.
func RunFileWatch(ctx context.Context, cfg Config, path string, emit Emitter) error {
	debounce := cfg.StabilityThreshold
	if debounce <= 0 {
		debounce = defaultFileDebounce
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	if err := w.Add(filepath.Dir(abs)); err != nil {
		return err
	}

	var lastAt time.Time
	emitDebounced := func(evType string) {
		now := time.Now()
		// Collapse any event within the debounce window into the first event of a burst.
		if now.Sub(lastAt) < debounce {
			return
		}
		lastAt = now
		emit(evType, path, nil)
	}

	// Initial check: existing file is reported as created.
	if _, err := os.Stat(abs); err == nil {
		emitDebounced("file.created")
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if ev.Name != abs {
				continue
			}
			switch {
			case ev.Op.Has(fsnotify.Create):
				emitDebounced("file.created")
			case ev.Op.Has(fsnotify.Write):
				emitDebounced("file.changed")
			case ev.Op.Has(fsnotify.Remove), ev.Op.Has(fsnotify.Rename):
				emitDebounced("file.removed")
			}
		case _, ok := <-w.Errors:
			if !ok {
				return nil
			}
			// fsnotify errors are transient on macOS; keep watching.
		}
	}
}
