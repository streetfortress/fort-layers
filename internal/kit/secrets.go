package kit

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// The plaintext half of a sops file. Key NAMES stay plaintext whatever
// the creation rule's encrypted_regex says, so a check can read which
// keys a host's secrets file or a flux Secret carries without an age
// key — the property the core's `fort check` runs on in CI.

// SopsHasKey reports whether an encrypted file declares a top-level key
// (a host's nixos/secrets/<host>.yaml is flat).
func SopsHasKey(path, key string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, key+":") {
			return true
		}
	}
	return false
}

// SecretDataKeys reads the stringData/data key names of a committed
// Secret (a flux file under the estate's encrypted_regex).
func SecretDataKeys(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		StringData map[string]any `yaml:"stringData"`
		Data       map[string]any `yaml:"data"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	var names []string
	for k := range doc.StringData {
		names = append(names, k)
	}
	for k := range doc.Data {
		names = append(names, k)
	}
	sort.Strings(names)
	return names, nil
}

// Exists says whether a repo-relative path is there.
func Exists(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, rel))
	return err == nil
}

// InventoryFile is the stack's seeding record; LegacyInventoryFile is
// its name before core v0.0.6 (oss/forts#88).
const (
	InventoryFile       = "forts.inventory.yaml"
	LegacyInventoryFile = "forts.contract.yaml"
)

// LegacyInventory says whether a stack directory still holds the record
// under its old name. The message mirrors the core's finding.
func LegacyInventory(root, dir string) (string, bool) {
	if Exists(root, filepath.Join(dir, LegacyInventoryFile)) && !Exists(root, filepath.Join(dir, InventoryFile)) {
		return "rename the file " + LegacyInventoryFile + " to " + InventoryFile + " (oss/forts#88)", true
	}
	return "", false
}
