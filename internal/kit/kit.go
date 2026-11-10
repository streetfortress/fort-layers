// Package kit is what the role binaries of fort-layers share: the
// estate root, the layer's files and their loaders, the plan an author
// verb prints and applies, the findings a check reports, the sops
// calls, and the text editors for the files a layer appends to.
//
// Copied from sfi/forts cmd/fort at 56ee5cd — the commit before
// forts#143 removed the layers from the core — and exported, so a role
// here holds the same helpers the core's verbs hold, without importing
// the core: its internal packages are closed, and a public Go API is a
// decision after v0.1 (forts#144). A helper that changes here changes
// for every role; a helper that changes in the core does not change
// here, and that is the point of the copy.
//
// The protocol a role speaks is forts' docs/layers.md: one binary,
// fort-<role>, with init, scaffold, mint, check and dismantle; one
// directory in the estate, layers/<role>/, with estate.toml and
// forts/<fort>.toml; the estate root in FORT_ESTATE_ROOT when the core
// execs the binary.
package kit

import (
	"fmt"
	"os"
	"path/filepath"
)

// RootEnv is the variable the core sets before it execs a role binary
// (forts' cmd/fort/layer.go): the estate root, so the role need not
// find it again.
const RootEnv = "FORT_ESTATE_ROOT"

// Root is the estate root: RootEnv when the core set it, else the
// nearest directory upward that holds .git — a role run by hand from
// anywhere in the checkout.
func Root() (string, error) {
	if r := os.Getenv(RootEnv); r != "" {
		if _, err := os.Stat(filepath.Join(r, "estate.toml")); err != nil {
			return "", fmt.Errorf("%s=%s is not an estate root: no estate.toml there", RootEnv, r)
		}
		return r, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "estate.toml")); err != nil {
				return "", fmt.Errorf("%s is a git checkout but not an estate: no estate.toml (fort init writes it)", dir)
			}
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not inside a git checkout — run a layer's verb from an estate, or through fort <role> <verb>")
		}
		dir = parent
	}
}
