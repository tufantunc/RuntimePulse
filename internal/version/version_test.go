package version

import (
	"strings"
	"testing"
)

func TestStringReturnsInjectedVersion(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	Version = "1.0.0"
	if got := String(); got != "1.0.0" {
		t.Fatalf("String() = %q, want injected version", got)
	}
}

func TestStringDevFallback(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	Version = "dev"
	got := String()
	// Test binaries have no VCS stamping, so plain "dev" is expected;
	// a source build of the real binary yields "dev (<rev>)".
	if !strings.HasPrefix(got, "dev") {
		t.Fatalf("String() = %q, want dev-prefixed fallback", got)
	}
}

func TestIsDev(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	Version = "dev"
	if !IsDev() {
		t.Fatal("dev must report IsDev")
	}
	Version = "1.0.0"
	if IsDev() {
		t.Fatal("release version must not report IsDev")
	}
}
