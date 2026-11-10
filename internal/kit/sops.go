package kit

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"forge.streetfortress.cloud/oss/fort-layers/internal/tools"
)

// Secrets is a role's only path to secret material: decrypt into
// memory, encrypt a plaintext file in place, set or unset one key in an
// encrypted file. The real one execs sops (and git add); a test swaps
// in a fake, so no test ever touches an age key or prints a value.
type Secrets interface {
	Decrypt(path string) ([]byte, error)
	EncryptInPlace(path string) error
	Set(path, key, value string) error
	UpdateKeys(path string) error
	GitAdd(path string) error
	// The host-secrets shapes (mint): the same operations under an
	// explicit config — nixos/.sops.yaml for nixos/secrets/<host>.yaml,
	// the estate's for a flux file — and a caller-supplied key path.
	EncryptWith(config, path string) error
	SetWith(config, path, jsonPath, value string) error
	// UnsetWith removes one key — the inverse of SetWith, and the way a
	// cross-fort declaration is withdrawn by a role's dismantle.
	UnsetWith(config, path, jsonPath string) error
}

// Sops is Secrets over the sops CLI, run from the estate root so the
// path-scoped creation rules of the estate's .sops.yaml apply.
type Sops struct{ Root string }

func (s Sops) Decrypt(path string) ([]byte, error) {
	cmd, err := tools.Command("sops", "-d", path)
	if err != nil {
		return nil, err
	}
	cmd.Dir = s.Root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sops -d %s: %w", path, err)
	}
	return out, nil
}

// EncryptInPlace runs under the estate's creation rules from the
// estate root, so the path-scoped rule for flux/apps/<fort>/ picks the
// fort's recipients (the identity the core's mint wrote).
func (s Sops) EncryptInPlace(path string) error {
	return s.EncryptWith(filepath.Join(s.Root, ".sops.yaml"), path)
}

func (s Sops) EncryptWith(config, path string) error {
	cmd, err := tools.Command("sops", "--config", config, "-e", "-i", path)
	if err != nil {
		return err
	}
	cmd.Dir = s.Root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sops -e %s: %s", path, LastLine(string(out)))
	}
	return nil
}

// Set writes one stringData key into an existing encrypted file: it
// re-encrypts just that value and the MAC, a reviewable diff.
func (s Sops) Set(path, key, value string) error {
	return s.SetWith(filepath.Join(s.Root, ".sops.yaml"), path, fmt.Sprintf(`["stringData"]["%s"]`, key), value)
}

func (s Sops) SetWith(config, path, jsonPath, value string) error {
	v, _ := json.Marshal(value)
	cmd, err := tools.Command("sops", "--config", config, "set", path, jsonPath, string(v))
	if err != nil {
		return err
	}
	cmd.Dir = s.Root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sops set %s %s: %s", path, jsonPath, LastLine(string(out)))
	}
	return nil
}

func (s Sops) UnsetWith(config, path, jsonPath string) error {
	cmd, err := tools.Command("sops", "--config", config, "unset", path, jsonPath)
	if err != nil {
		return err
	}
	cmd.Dir = s.Root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sops unset %s %s: %s", path, jsonPath, LastLine(string(out)))
	}
	return nil
}

// UpdateKeys re-encrypts a file to the recipients its creation rule
// now lists — removing a recipient from .sops.yaml alone changes
// nothing: the departed key keeps decrypting until this runs.
func (s Sops) UpdateKeys(path string) error {
	cmd, err := tools.Command("sops", "--config", filepath.Join(s.Root, ".sops.yaml"), "updatekeys", "-y", path)
	if err != nil {
		return err
	}
	cmd.Dir = s.Root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sops updatekeys %s: %s", path, LastLine(string(out)))
	}
	return nil
}

// GitAdd stages a path — additions, edits and deletions alike (-A).
func (s Sops) GitAdd(path string) error {
	cmd, err := tools.Command("git", "add", "-A", "--", path)
	if err != nil {
		return err
	}
	cmd.Dir = s.Root
	return cmd.Run()
}
