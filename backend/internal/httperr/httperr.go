// Package httperr provides server-error sanitization: log the full error
// with a request-scoped ID, return a generic message + that ID to the client.
package httperr

import (
	"crypto/rand"
	"encoding/hex"
	"log"
)

// ErrorID returns a short random ID suitable for correlating a client-facing
// error with a server log line.
func ErrorID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Log records the full internal error against a generated ID and returns the
// ID + a user-facing message. Callers should use the returned tuple in their
// writeJSON/writeError response.
func Log(where string, err error) (id, userMessage string) {
	id = ErrorID()
	log.Printf("[err %s] %s: %v", id, where, err)
	userMessage = "An internal error occurred. Reference: " + id
	return
}
