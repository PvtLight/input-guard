package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Protector seals the verifier for this Windows user. The app uses DPAPI
// (CryptProtectData); tests use a stand-in.
type Protector interface {
	Protect(plain []byte) ([]byte, error)
	Unprotect(sealed []byte) ([]byte, error)
}

// fileMagic starts the passphrase file, before the sealed record.
var fileMagic = []byte("InputGuardVerifier1\n")

// Vault is the passphrase file: %LOCALAPPDATA%\Input Guard\passphrase.dat in
// the app. It holds the salted verifier sealed with DPAPI, never the
// passphrase. Deleting the file is a reset.
type Vault struct {
	Path      string
	Protector Protector
}

// Read returns nil with no error when no passphrase is saved yet.
func (v Vault) Read() (*Record, error) {
	data, err := os.ReadFile(v.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("Could not read the saved passphrase (" + err.Error() + "). Input Guard will not lock.")
	}
	if !bytes.HasPrefix(data, fileMagic) {
		return nil, ErrInvalidRecord
	}
	plain, err := v.Protector.Unprotect(data[len(fileMagic):])
	if err != nil {
		return nil, errors.New("Windows could not unseal the saved passphrase for this user. Input Guard will not lock. Set the passphrase again in Settings.")
	}
	defer clear(plain)
	var record Record
	if err := json.Unmarshal(plain, &record); err != nil {
		return nil, ErrInvalidRecord
	}
	if err := record.Validate(); err != nil {
		return nil, err
	}
	return &record, nil
}

// Save replaces the file in one step, so a crash never leaves half a file.
func (v Vault) Save(record Record) error {
	if err := record.Validate(); err != nil {
		return err
	}
	plain, err := json.Marshal(record)
	if err != nil {
		return err
	}
	defer clear(plain)
	sealed, err := v.Protector.Protect(plain)
	if err != nil {
		return errors.New("Windows could not seal the passphrase (" + err.Error() + ").")
	}
	if err := os.MkdirAll(filepath.Dir(v.Path), 0o700); err != nil {
		return errors.New("Could not create the Input Guard folder (" + err.Error() + ").")
	}
	temp, err := os.CreateTemp(filepath.Dir(v.Path), "passphrase-*.tmp")
	if err != nil {
		return errors.New("Could not save the passphrase (" + err.Error() + ").")
	}
	_, writeErr := temp.Write(append(append([]byte{}, fileMagic...), sealed...))
	syncErr := temp.Sync()
	closeErr := temp.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		os.Remove(temp.Name())
		return errors.New("Could not save the passphrase (" + err.Error() + ").")
	}
	if err := os.Rename(temp.Name(), v.Path); err != nil {
		os.Remove(temp.Name())
		return errors.New("Could not save the passphrase (" + err.Error() + ").")
	}
	return nil
}

// Settings are the few choices kept between launches, in settings.json next
// to the passphrase file. The lock itself is never saved.
type Settings struct {
	AllowAutomation bool `json:"allowAutomation"`
}

func ReadSettings(path string) Settings {
	var s Settings
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

func SaveSettings(path string, s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
