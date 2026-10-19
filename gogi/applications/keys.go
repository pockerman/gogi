package applications

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

const (
	keyPrefix = "gogi_"
	// how much of a key is kept to tell keys apart: "gogi_" and 7 characters
	shownLength = len(keyPrefix) + 7
)

// NewKey is a new API key: "gogi_" and 32 random bytes
func NewKey() string {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		// crypto/rand does not fail on the platforms gogi runs on
		panic(err)
	}
	return keyPrefix + base64.RawURLEncoding.EncodeToString(secret)
}

// Hash is what is kept of a key. Keys are random, so a plain SHA-256 cannot be reversed
func Hash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// Shown is the part of a key that is kept, to tell keys apart
func Shown(key string) string {
	if len(key) < shownLength {
		return key
	}
	return key[:shownLength]
}

// looksLikeKey tells whether the text can be a key gogi gave, without looking it up
func looksLikeKey(text string) bool {
	return strings.HasPrefix(text, keyPrefix) && len(text) > shownLength
}
