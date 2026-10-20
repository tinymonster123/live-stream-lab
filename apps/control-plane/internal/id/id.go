// Package id generates identifiers shared by the control-plane modules.
package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// New returns a random 64-bit hex identifier. It falls back to the clock when
// the system entropy source fails, matching the original control-plane
// behaviour.
func New() string {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes[:])
}
