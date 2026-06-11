package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func startFileWatch(t *testing.T, path string) *recorder {
	t.Helper()
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		if err := RunFileWatch(ctx, Config{}, path, rec.emit); err != nil && ctx.Err() == nil {
			t.Errorf("RunFileWatch: %v", err)
		}
	}()
	time.Sleep(100 * time.Millisecond) // let the fsnotify watch arm
	return rec
}

func TestFileWatchLifecycle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dist.js")
	rec := startFileWatch(t, path)

	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	evs := rec.waitLen(t, 1)
	if evs[0] != "file.created" {
		t.Fatalf("want file.created, got %v", evs)
	}

	time.Sleep(600 * time.Millisecond) // past default debounce
	if err := os.WriteFile(path, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	evs = rec.waitLen(t, 2)
	if evs[1] != "file.changed" {
		t.Fatalf("want file.changed, got %v", evs)
	}

	time.Sleep(600 * time.Millisecond)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	evs = rec.waitLen(t, 3)
	if evs[2] != "file.removed" {
		t.Fatalf("want file.removed, got %v", evs)
	}
}

func TestFileWatchInitialExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "already.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := startFileWatch(t, path)
	evs := rec.waitLen(t, 1)
	if evs[0] != "file.created" {
		t.Fatalf("existing file must emit initial file.created, got %v", evs)
	}
}

func TestFileWatchDebouncesBursts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "burst.txt")
	rec := startFileWatch(t, path)
	for i := 0; i < 5; i++ { // editor-style write burst
		if err := os.WriteFile(path, []byte{byte(i)}, 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(700 * time.Millisecond)
	evs := rec.snapshot()
	if len(evs) != 1 {
		t.Fatalf("burst must collapse to one event, got %v", evs)
	}
}
