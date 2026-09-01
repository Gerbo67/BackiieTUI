package idgen

import "github.com/google/uuid"

// New returns a new random UUID v4 string.
func New() string {
	return uuid.New().String()
}
