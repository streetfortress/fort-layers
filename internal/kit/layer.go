package kit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Dir is the directory the layers own in an estate; a role owns one
// subdirectory of it, and the core never reads under it.
const Dir = "layers"

// CoreFortsDir is the core's manifests, forts/<fort>.toml: the names a
// layer's per-fort file may take are the names the core declares.
const CoreFortsDir = "forts"

// Layer is one role by name: the binary is fort-<name>, the directory
// is layers/<name>/.
type Layer string

// Dir is the role's directory, repo-relative.
func (l Layer) Dir() string { return filepath.Join(Dir, string(l)) }

// EstatePath is the role's estate declarations, repo-relative.
func (l Layer) EstatePath() string { return filepath.Join(l.Dir(), "estate.toml") }

// FortsDir is the role's per-fort files, repo-relative.
func (l Layer) FortsDir() string { return filepath.Join(l.Dir(), "forts") }

// FortPath is the role's declarations for one fort, repo-relative.
func (l Layer) FortPath(fort string) string { return filepath.Join(l.FortsDir(), fort+".toml") }

// Installed says whether the role's init has run in the estate: its
// estate.toml exists. The core's check warns on a layer directory
// without one.
func (l Layer) Installed(root string) bool {
	_, err := os.Stat(filepath.Join(root, l.EstatePath()))
	return err == nil
}

// LoadEstate decodes the role's estate.toml into v, strictly: a key v
// does not carry is an error, so a typo is a refusal and not a default
// (the core's manifests hold the same line).
func (l Layer) LoadEstate(root string, v any) error {
	return DecodeStrict(filepath.Join(root, l.EstatePath()), v)
}

// LoadFort decodes the role's file for one fort into v, strictly.
func (l Layer) LoadFort(root, fort string, v any) error {
	if !ValidFortName(fort) {
		return fmt.Errorf("%q is not a fort name", fort)
	}
	return DecodeStrict(filepath.Join(root, l.FortPath(fort)), v)
}

// FortNames lists the forts the role has a file for, sorted. No
// directory is no forts, not an error: a role installed and not yet
// scaffolded on any fort.
func (l Layer) FortNames(root string) ([]string, error) {
	return tomlNames(filepath.Join(root, l.FortsDir()))
}

// CoreForts lists the core's manifests, forts/*.toml, sorted: every
// fort the estate declares. A layer's per-fort file for a name the
// core does not declare is a stray, and the role's check says so.
func CoreForts(root string) ([]string, error) {
	names, err := tomlNames(filepath.Join(root, CoreFortsDir))
	if err != nil {
		return nil, err
	}
	if names == nil {
		return nil, fmt.Errorf("no %s/ directory — this is not an estate (fort init writes it)", CoreFortsDir)
	}
	return names, nil
}

// Decode decodes one TOML file into v, loosely: a key v does not carry
// is ignored. For a file whose keys are someone else's.
func Decode(path string, v any) error {
	if _, err := toml.DecodeFile(path, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// CoreEstate decodes the core's estate.toml into v, loosely: the
// core's keys are the core's, and a role reads the few it needs (its
// [[serves.<role>]] entries and their ports, its [hubs.<role>]) from a
// struct that names only those.
func CoreEstate(root string, v any) error {
	return Decode(filepath.Join(root, "estate.toml"), v)
}

// DecodeStrict decodes one TOML file into v and refuses a key v does
// not carry.
func DecodeStrict(path string, v any) error {
	md, err := toml.DecodeFile(path, v)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if un := md.Undecoded(); len(un) > 0 {
		keys := make([]string, len(un))
		for i, k := range un {
			keys[i] = k.String()
		}
		return fmt.Errorf("%s: unknown key(s) %s", path, strings.Join(keys, ", "))
	}
	return nil
}

func tomlNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".toml") {
			names = append(names, strings.TrimSuffix(e.Name(), ".toml"))
		}
	}
	sort.Strings(names)
	return names, nil
}

var fortRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ValidFortName is the core's rule for a host name: lowercase labels
// and dashes, no leading or trailing dash.
func ValidFortName(s string) bool { return fortRE.MatchString(s) }

// DefaultInput is the flake input name `fort layer add` gives this
// repository when the estate does not rename it.
const DefaultInput = "fort-layers"

// Input is the flake input the estate holds this layer under: what
// `fort layer add <url>#<role>` named it, read from nixos/flake.nix.
func (l Layer) Input(root string) string {
	raw, err := os.ReadFile(filepath.Join(root, "nixos/flake.nix"))
	if err != nil {
		return DefaultInput
	}
	re := regexp.MustCompile(`fort-` + regexp.QuoteMeta(string(l)) + `\s*=\s*inputs\.([a-z0-9-]+)\.packages`)
	if m := re.FindSubmatch(raw); m != nil {
		return string(m[1])
	}
	return DefaultInput
}
