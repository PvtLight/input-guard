// Package core is the part of Input Guard for Windows that does not depend on
// Windows: the passphrase verifier, the unlock prompt, the attempt limits, the
// five-click unlock, the rules the input hooks follow, the passphrase file and
// the recovery helper. `go test ./core` runs it on any computer.
package core

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
)

const (
	MinPassphrase = 4
	MaxPassphrase = 128

	recordVersion = 1
	recordRounds  = 600_000
	recordBytes   = 32
)

var (
	ErrPassphraseRules = errors.New("Use 4–128 characters: letters, numbers, spaces, or punctuation from an English keyboard. Use a separate passphrase, not your Windows password.")
	ErrInvalidRecord   = errors.New("The saved passphrase record is invalid. Input Guard will not lock.")
)

// Record is the saved passphrase verifier, the same as the Mac app's: a random
// 32-byte salt and a PBKDF2-HMAC-SHA256 digest with 600,000 rounds. The
// passphrase itself is never saved.
type Record struct {
	Version int    `json:"version"`
	Salt    []byte `json:"salt"`
	Digest  []byte `json:"digest"`
	Rounds  int    `json:"rounds"`
}

func Derive(passphrase string, salt []byte, rounds int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, passphrase, salt, rounds, recordBytes)
}

// ValidPassphrase allows 4–128 printable ASCII characters, which the unlock
// prompt can always type with an English keyboard layout.
func ValidPassphrase(passphrase string) bool {
	if len(passphrase) < MinPassphrase || len(passphrase) > MaxPassphrase {
		return false
	}
	for i := 0; i < len(passphrase); i++ {
		if passphrase[i] < 32 || passphrase[i] > 126 {
			return false
		}
	}
	return true
}

func NewRecord(passphrase string) (Record, error) {
	if !ValidPassphrase(passphrase) {
		return Record{}, ErrPassphraseRules
	}
	salt := make([]byte, recordBytes)
	if _, err := rand.Read(salt); err != nil {
		return Record{}, errors.New("Could not generate a secure salt.")
	}
	digest, err := Derive(passphrase, salt, recordRounds)
	if err != nil {
		return Record{}, errors.New("Could not prepare the passphrase.")
	}
	return Record{Version: recordVersion, Salt: salt, Digest: digest, Rounds: recordRounds}, nil
}

func (r Record) Validate() error {
	if r.Version != recordVersion || len(r.Salt) != recordBytes || len(r.Digest) != recordBytes || r.Rounds != recordRounds {
		return ErrInvalidRecord
	}
	return nil
}

// Matches is false for an invalid record, so a damaged file never unlocks.
func (r Record) Matches(candidate string) bool {
	if r.Validate() != nil || candidate == "" || len(candidate) > MaxPassphrase {
		return false
	}
	actual, err := Derive(candidate, r.Salt, r.Rounds)
	return err == nil && subtle.ConstantTimeCompare(actual, r.Digest) == 1
}
