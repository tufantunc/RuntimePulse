// Package watch hosts runtime condition observers (spec §4.2).
package watch

import (
	"fmt"
	"time"
)

// Config controls polling cadence and flap suppression.
// Durations marshal as nanoseconds in JSON (stored in the watches table).
type Config struct {
	Interval           time.Duration `json:"interval,omitempty"`
	StabilityThreshold time.Duration `json:"stabilityThreshold,omitempty"`
}

// WithDefaults fills zero values; a zero threshold means transitions
// emit immediately (no debounce).
func (c Config) WithDefaults() Config {
	if c.Interval <= 0 {
		c.Interval = 2 * time.Second
	}
	return c
}

// Watch is a persisted runtime condition observer.
type Watch struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // http|tcp|file|process|docker|git
	Target    string    `json:"target"`
	Config    Config    `json:"config"`
	CreatedAt time.Time `json:"createdAt"`
}

// Emitter delivers a watcher observation into the engine.
type Emitter func(evType, source string, payload map[string]string)

var validTypes = map[string]bool{
	"http": true, "tcp": true, "file": true, "process": true, "docker": true, "git": true,
}

func ValidateType(t string) error {
	if !validTypes[t] {
		return fmt.Errorf("unknown watch type %q (valid: http, tcp, file, process, docker, git)", t)
	}
	return nil
}
