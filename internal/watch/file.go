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
// Debounce is TRAILING-EDGE: fs events reset a timer; when the burst
// goes quiet for the debounce window (Config.StabilityThreshold,
// default 500ms), one event is emitted reflecting the file's final
// state — so rm+recreate bursts report the truth (file.changed), never
// a stale file.removed. Blocks until ctx is cancelled.
func RunFileWatch(ctx context.Context, cfg Config, path string, emit Emitter) error {
	debounce := cfg.StabilityThreshold
	if debounce <= 0 {
		debounce = defaultFileDebounce
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	parent := filepath.Dir(abs)
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	if err := w.Add(parent); err != nil {
		return err
	}

	exists := false
	if _, err := os.Stat(abs); err == nil {
		exists = true
		emit("file.created", path, nil) // initial check
	}

	timer := time.NewTimer(debounce)
	if !timer.Stop() {
		<-timer.C
	}
	var timerC <-chan time.Time // nil until a burst is pending

	rearmParent := func() {
		// The parent directory vanished (rm -rf dist/): the kernel watch
		// silently dies. Report the file gone, then poll until the parent
		// reappears and re-arm the watch.
		if exists {
			exists = false
			emit("file.removed", path, nil)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(debounce):
			}
			if err := w.Add(parent); err == nil {
				if _, err := os.Stat(abs); err == nil {
					exists = true
					emit("file.created", path, nil)
				}
				return
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if ev.Name == parent && (ev.Op.Has(fsnotify.Remove) || ev.Op.Has(fsnotify.Rename)) {
				rearmParent()
				continue
			}
			if ev.Name != abs ||
				!(ev.Op.Has(fsnotify.Create) || ev.Op.Has(fsnotify.Write) ||
					ev.Op.Has(fsnotify.Remove) || ev.Op.Has(fsnotify.Rename)) {
				continue // Chmod and unrelated siblings stay ignored
			}
			timer.Reset(debounce)
			timerC = timer.C
		case <-timerC:
			timerC = nil
			_, statErr := os.Stat(abs)
			nowExists := statErr == nil
			switch {
			case nowExists && !exists:
				emit("file.created", path, nil)
			case nowExists && exists:
				emit("file.changed", path, nil)
			case !nowExists && exists:
				emit("file.removed", path, nil)
			} // transient file (!nowExists && !exists): net no-op
			exists = nowExists
		case _, ok := <-w.Errors:
			if !ok {
				return nil
			}
		}
	}
}
