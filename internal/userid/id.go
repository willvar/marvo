package userid

import (
	"crypto/rand"
	"encoding/hex"
)

const (
	Length      = 20
	randomBytes = Length / 2
)

func New() (string, error) {
	var raw [randomBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func Valid(id string) bool {
	if len(id) != Length {
		return false
	}
	for _, char := range id {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}
