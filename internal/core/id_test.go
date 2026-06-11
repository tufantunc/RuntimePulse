package core

import (
	"strings"
	"testing"
)

func TestNewID(t *testing.T) {
	a := NewID("evt")
	b := NewID("evt")
	if a == b {
		t.Fatalf("ids must be unique, got %q twice", a)
	}
	if !strings.HasPrefix(a, "evt-") {
		t.Fatalf("id must start with prefix, got %q", a)
	}
	if len(a) != len("evt-")+12 {
		t.Fatalf("id must have 12 hex chars after prefix, got %q", a)
	}
}
