package core

import (
	"testing"
	"time"
)

func TestEventSelectorMatches(t *testing.T) {
	ev := Event{Type: "docker.healthy", Source: "postgres"}
	cases := []struct {
		name string
		sel  EventSelector
		want bool
	}{
		{"type+source match", EventSelector{Type: "docker.healthy", Source: "postgres"}, true},
		{"empty source matches any", EventSelector{Type: "docker.healthy"}, true},
		{"wrong source", EventSelector{Type: "docker.healthy", Source: "redis"}, false},
		{"wrong type", EventSelector{Type: "http.available", Source: "postgres"}, false},
	}
	for _, c := range cases {
		if got := c.sel.Matches(ev); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestRuleActive(t *testing.T) {
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	cases := []struct {
		name string
		rule Rule
		want bool
	}{
		{"no expiry, not consumed", Rule{}, true},
		{"consumed", Rule{Consumed: true}, false},
		{"expires in future", Rule{ExpiresAt: &future}, true},
		{"expired", Rule{ExpiresAt: &past}, false},
	}
	for _, c := range cases {
		if got := c.rule.Active(now); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
