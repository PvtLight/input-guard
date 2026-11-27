package core

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sealer stands in for DPAPI: it flips the bytes and adds a check byte, so a
// changed file fails to unseal, as DPAPI's does.
type sealer struct{ broken bool }

func (s sealer) Protect(plain []byte) ([]byte, error) {
	if s.broken {
		return nil, errors.New("no DPAPI here")
	}
	out, sum := make([]byte, len(plain)+1), byte(0)
	for i, b := range plain {
		out[i] = ^b
		sum += b
	}
	out[len(plain)] = sum
	return out, nil
}

func (s sealer) Unprotect(sealed []byte) ([]byte, error) {
	if len(sealed) == 0 {
		return nil, errors.New("empty")
	}
	plain, sum := make([]byte, len(sealed)-1), byte(0)
	for i, b := range sealed[:len(sealed)-1] {
		plain[i] = ^b
		sum += plain[i]
	}
	if sum != sealed[len(sealed)-1] {
		return nil, errors.New("tampered")
	}
	return plain, nil
}

func TestVault(t *testing.T) {
	dir := t.TempDir()
	vault := Vault{Path: filepath.Join(dir, "Input Guard", "passphrase.dat"), Protector: sealer{}}
	if record, err := vault.Read(); record != nil || err != nil {
		t.Fatalf("no file means no passphrase yet: %v %v", record, err)
	}
	record, err := NewRecord("Open sesame 42")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Save(record); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(vault.Path)
	if bytes.Contains(data, []byte("Open sesame")) || bytes.Contains(data, []byte("digest")) || !bytes.HasPrefix(data, fileMagic) {
		t.Error("the file holds only the sealed verifier")
	}
	read, err := vault.Read()
	if err != nil || read == nil || !read.Matches("Open sesame 42") || read.Matches("open sesame 42") {
		t.Fatalf("the saved verifier checks the passphrase: %v", err)
	}
	replacement, _ := NewRecord("new one")
	if err := vault.Save(replacement); err != nil {
		t.Fatal(err)
	}
	if read, _ := vault.Read(); read == nil || !read.Matches("new one") || read.Matches("Open sesame 42") {
		t.Error("saving again replaces the passphrase")
	}
	entries, _ := os.ReadDir(filepath.Dir(vault.Path))
	if len(entries) != 1 {
		t.Errorf("no temporary files are left behind: %v", entries)
	}

	data, _ = os.ReadFile(vault.Path)
	data[len(data)-5] ^= 0x40
	os.WriteFile(vault.Path, data, 0o600)
	if read, err := vault.Read(); read != nil || err == nil || !strings.Contains(err.Error(), "will not lock") {
		t.Errorf("a changed file refuses to lock: %v", err)
	}
	os.WriteFile(vault.Path, []byte("something else"), 0o600)
	if read, err := vault.Read(); read != nil || err != ErrInvalidRecord {
		t.Errorf("a file that isn't ours refuses to lock: %v", err)
	}
	weak := replacement
	weak.Rounds = 1000
	sealed, _ := sealer{}.Protect([]byte(`{"version":1,"salt":"` + strings.Repeat("A", 43) + `=","digest":"` + strings.Repeat("A", 43) + `=","rounds":1000}`))
	os.WriteFile(vault.Path, append(append([]byte{}, fileMagic...), sealed...), 0o600)
	if read, err := vault.Read(); read != nil || err != ErrInvalidRecord {
		t.Errorf("a weaker record refuses to lock: %v", err)
	}
	if err := vault.Save(weak); err != ErrInvalidRecord {
		t.Error("an invalid record is never saved")
	}
	if err := (Vault{Path: vault.Path, Protector: sealer{broken: true}}).Save(replacement); err == nil {
		t.Error("a sealing failure is reported")
	}

	settings := filepath.Join(dir, "Input Guard", "settings.json")
	if ReadSettings(settings).AllowAutomation {
		t.Error("automation events are off by default")
	}
	if err := SaveSettings(settings, Settings{AllowAutomation: true}); err != nil || !ReadSettings(settings).AllowAutomation {
		t.Errorf("settings are kept: %v", err)
	}
}
