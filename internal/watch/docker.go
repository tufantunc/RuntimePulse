package watch

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

// MapDockerEvent maps one `docker events --format '{{json .}}'` line to
// a RuntimePulse event type; "" means ignore the line.
func MapDockerEvent(line []byte) (string, map[string]string) {
	var ev struct {
		Type   string `json:"Type"`
		Action string `json:"Action"`
		Actor  struct {
			Attributes map[string]string `json:"Attributes"`
		} `json:"Actor"`
	}
	if err := json.Unmarshal(line, &ev); err != nil || ev.Type != "container" {
		return "", nil
	}
	payload := map[string]string{"container": ev.Actor.Attributes["name"]}
	switch {
	case ev.Action == "start":
		return "docker.started", payload
	case ev.Action == "die" || ev.Action == "stop" || ev.Action == "kill":
		return "docker.stopped", payload
	case ev.Action == "health_status: healthy":
		return "docker.healthy", payload
	case ev.Action == "health_status: unhealthy":
		return "docker.unhealthy", payload
	}
	return "", nil
}

// MapDockerInspect maps `docker inspect --format '{{.State.Status}}
// {{if .State.Health}}{{.State.Health.Status}}{{end}}'` output to an
// event type for the initial check; "" means unknown.
func MapDockerInspect(out string) string {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) == 0 {
		return ""
	}
	status := fields[0]
	health := ""
	if len(fields) > 1 {
		health = fields[1]
	}
	switch {
	case status == "running" && health == "healthy":
		return "docker.healthy"
	case status == "running" && health == "unhealthy":
		return "docker.unhealthy"
	case status == "running":
		return "docker.started"
	default:
		return "docker.stopped"
	}
}

// RunDockerWatch performs the initial check via `docker inspect`, then
// streams `docker events` for the container, mapping lines to events.
// If the subprocess exits (docker daemon restart, …) it re-inspects
// (catching transitions missed in the gap) and restarts the stream
// after a backoff. Requires the docker CLI on PATH (validated at watch
// creation). Blocks until ctx is cancelled.
func RunDockerWatch(ctx context.Context, cfg Config, container string, emit Emitter) error {
	var lastType string
	emitOnce := func(evType string, payload map[string]string) {
		if evType == "" || evType == lastType {
			return // edge semantics: suppress repeats of the same state
		}
		lastType = evType
		emit(evType, container, payload)
	}

	inspect := func() {
		out, err := exec.CommandContext(ctx, "docker", "inspect", "--format",
			"{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}", container).Output()
		if err != nil {
			return // container may not exist yet; the events stream will catch creation
		}
		emitOnce(MapDockerInspect(string(out)), map[string]string{"container": container})
	}

	for {
		inspect()
		cmd := exec.CommandContext(ctx, "docker", "events",
			"--filter", "container="+container, "--format", "{{json .}}")
		stdout, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err == nil {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				evType, payload := MapDockerEvent(scanner.Bytes())
				emitOnce(evType, payload)
			}
			cmd.Wait() //nolint:errcheck
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(2 * time.Second): // backoff, then re-inspect + restart stream
		}
	}
}

// DockerAvailable reports whether the docker CLI is usable; watch
// creation refuses docker watches without it.
func DockerAvailable() error {
	_, err := exec.LookPath("docker")
	return err
}
