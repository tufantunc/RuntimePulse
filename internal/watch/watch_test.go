package watch

import (
	"testing"
	"time"
)

func TestConfigDefaults(t *testing.T) {
	c := Config{}.WithDefaults()
	if c.Interval != 2*time.Second {
		t.Fatalf("default interval = %v", c.Interval)
	}
	c2 := Config{Interval: time.Second, StabilityThreshold: 5 * time.Second}.WithDefaults()
	if c2.Interval != time.Second || c2.StabilityThreshold != 5*time.Second {
		t.Fatalf("explicit config overwritten: %#v", c2)
	}
}

func TestValidateType(t *testing.T) {
	for _, typ := range []string{"http", "tcp", "file", "process", "docker", "git"} {
		if err := ValidateType(typ); err != nil {
			t.Fatalf("%s must be valid: %v", typ, err)
		}
	}
	if err := ValidateType("carrier-pigeon"); err == nil {
		t.Fatal("unknown type must error")
	}
}
