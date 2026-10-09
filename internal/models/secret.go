package models

import "time"

// CreateSecretRequest carries a secret encrypted in the browser. The server
// never sees the plaintext, the password, or the key held in the link.
// Binary fields are standard base64.
type CreateSecretRequest struct {
	Ciphertext string `json:"ciphertext" binding:"required"`
	IV         string `json:"iv" binding:"required"`
	Salt       string `json:"salt" binding:"required"`
	Iterations int    `json:"iterations" binding:"required"`
	// Auth is a token derived from the password and link key; the server
	// stores only its hash and requires it to release the ciphertext.
	Auth string `json:"auth" binding:"required"`
	// TTL is one of "1d", "7d", "30d".
	TTL string `json:"ttl" binding:"required"`
}

type CreateSecretResponse struct {
	ID        string    `json:"id"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// SecretMeta is what a recipient needs to derive keys before revealing.
type SecretMeta struct {
	Salt         string    `json:"salt"`
	Iterations   int       `json:"iterations"`
	ExpiresAt    time.Time `json:"expiresAt"`
	AttemptsLeft int       `json:"attemptsLeft"`
}

type RevealSecretRequest struct {
	Auth string `json:"auth" binding:"required"`
}

type RevealedSecret struct {
	Ciphertext string `json:"ciphertext"`
	IV         string `json:"iv"`
}
