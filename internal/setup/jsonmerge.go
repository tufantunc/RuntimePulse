// Package setup registers RuntimePulse as an MCP server with the agent
// CLIs installed on the machine (the `runtimepulse setup` wizard).
package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// mergeJSONServer sets config[section][name] = entry in the JSON file at
// path, preserving every other key, creating the file and parent dirs
// when absent, and writing atomically. It refuses to touch a file that
// is not valid JSON (never clobber what we can't understand).
func mergeJSONServer(path, section, name string, entry map[string]any) error {
	root := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		if err := dec.Decode(&root); err != nil {
			return fmt.Errorf("setup: %s is not valid JSON, leaving it untouched: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	sec, ok := root[section].(map[string]any)
	if !ok {
		sec = map[string]any{}
		root[section] = sec
	}
	sec[name] = entry

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	return atomicWrite(path, out)
}

// atomicWrite writes data to path via a temp file in the same directory
// plus os.Rename, creating parent dirs (0o700). The rename is atomic on
// the same filesystem; the temp is cleaned up if the rename never happens.
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".rp-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// hasJSONServer reports whether config[section][name] already exists.
func hasJSONServer(path, section, name string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var root map[string]any
	if json.Unmarshal(b, &root) != nil {
		return false
	}
	sec, ok := root[section].(map[string]any)
	if !ok {
		return false
	}
	_, ok = sec[name]
	return ok
}
