package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// NewID returns prefix + "-" + 12 random hex chars, e.g. "evt-a1b2c3d4e5f6".
func NewID(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return prefix + "-" + hex.EncodeToString(b)
}
