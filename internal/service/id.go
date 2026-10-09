package service

import (
	"crypto/rand"
	"encoding/base64"
)

const idBytes = 16

// newRandomID returns an unguessable 128-bit URL-safe identifier.
func newRandomID() (string, error) {
	raw := make([]byte, idBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func validRandomID(id string) bool {
	b, err := base64.RawURLEncoding.DecodeString(id)
	return err == nil && len(b) == idBytes
}
