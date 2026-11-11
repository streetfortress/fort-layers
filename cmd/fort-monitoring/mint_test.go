package main

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeSecrets is kit.Secrets over plaintext files: the encrypt and git
// calls are recorded. No test touches an age key.
type fakeSecrets struct {
	root      string
	encrypted []string
	added     []string
}

func (f *fakeSecrets) Decrypt(path string) ([]byte, error) { return os.ReadFile(f.full(path)) }
func (f *fakeSecrets) EncryptInPlace(path string) error {
	f.encrypted = append(f.encrypted, path)
	return nil
}
func (f *fakeSecrets) EncryptWith(config, path string) error              { return f.EncryptInPlace(path) }
func (f *fakeSecrets) Set(path, key, value string) error                  { return nil }
func (f *fakeSecrets) SetWith(config, path, jsonPath, value string) error { return nil }
func (f *fakeSecrets) UnsetWith(config, path, jsonPath string) error      { return nil }
func (f *fakeSecrets) UpdateKeys(path string) error                       { return nil }
func (f *fakeSecrets) GitAdd(path string) error {
	f.added = append(f.added, path)
	return nil
}
func (f *fakeSecrets) full(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(f.root, path)
}

func TestMintWritesTheHubsCredential(t *testing.T) {
	root := estate(t)
	scaffoldAll(t, root)
	tool := &fakeSecrets{root: root}

	p, err := planMint(root, "hub")
	if err != nil {
		t.Fatal(err)
	}
	if p.present {
		t.Error("present before the write")
	}
	if err := p.apply(tool); err != nil {
		t.Fatal(err)
	}
	rel := filepath.Join(stackDir("hub"), stackSecrets[0])
	if !has(tool.encrypted, rel) || !has(tool.added, rel) {
		t.Errorf("%s not encrypted/staged: %v %v", rel, tool.encrypted, tool.added)
	}
	if s := read(t, root, rel); !contains(s, "admin-user: admin", "admin-password: "+p.password, "name: grafana-admin") {
		t.Errorf("grafana:\n%s", s)
	}
	// The gate clears its warning. With kustomize on PATH the stack
	// builds from the plaintext file; without it, a warning.
	s, err := gate(t)
	if err != nil || contains(s, "fort monitoring mint") {
		t.Errorf("check --all after mint: %v\n%s", err, s)
	}
	// A re-run keeps the file.
	p, err = planMint(root, "hub")
	if err != nil {
		t.Fatal(err)
	}
	if !p.present {
		t.Error("not present on re-run")
	}
	before := read(t, root, rel)
	if err := p.apply(tool); err != nil {
		t.Fatal(err)
	}
	if read(t, root, rel) != before {
		t.Error("a re-run rewrote the file")
	}
}

func TestMintRefusals(t *testing.T) {
	root := estate(t)
	scaffoldAll(t, root)
	if _, err := planMint(root, "edge"); err == nil || !contains(err.Error(), "no [monitoring.hub]") {
		t.Errorf("watched fort: %v", err)
	}
	os.RemoveAll(filepath.Join(root, stackDir("hub")))
	if _, err := planMint(root, "hub"); err == nil || !contains(err.Error(), "renders the stack") {
		t.Errorf("no stack: %v", err)
	}
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
