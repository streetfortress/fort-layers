package kit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// estate lays down the shape a role reads: the core's estate.toml and
// forts/, and one layer with an estate file and one fort file.
func estate(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(path, text string) {
		t.Helper()
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("estate.toml", "name = \"test\"\n[[serves.backups]]\nfort = \"hub\"\nports = [30900]\n")
	write("forts/hub.toml", "mesh_ip = \"10.9.0.2\"\n")
	write("forts/edge.toml", "mesh_ip = \"10.9.0.3\"\n")
	write("layers/example/estate.toml", "# the layer's estate\nstore = \"b2\"\n")
	write("layers/example/forts/hub.toml", "every = \"daily\"\n")
	return root
}

func TestLayerPaths(t *testing.T) {
	l := Layer("example")
	if l.Dir() != "layers/example" || l.EstatePath() != "layers/example/estate.toml" || l.FortPath("hub") != "layers/example/forts/hub.toml" {
		t.Errorf("paths: %s %s %s", l.Dir(), l.EstatePath(), l.FortPath("hub"))
	}
}

func TestLoaders(t *testing.T) {
	root := estate(t)
	l := Layer("example")
	if !l.Installed(root) || Layer("other").Installed(root) {
		t.Error("Installed: example yes, other no")
	}
	var e struct{ Store string }
	if err := l.LoadEstate(root, &e); err != nil || e.Store != "b2" {
		t.Errorf("LoadEstate: %+v %v", e, err)
	}
	var f struct{ Every string }
	if err := l.LoadFort(root, "hub", &f); err != nil || f.Every != "daily" {
		t.Errorf("LoadFort: %+v %v", f, err)
	}
	// Strict: a key the struct lacks is a refusal that names it.
	var narrow struct{}
	if err := l.LoadEstate(root, &narrow); err == nil || !strings.Contains(err.Error(), "unknown key(s) store") {
		t.Errorf("strict decode: %v", err)
	}
	if err := l.LoadFort(root, "../estate", &f); err == nil {
		t.Error("a path is not a fort name")
	}
	names, err := l.FortNames(root)
	if err != nil || strings.Join(names, ",") != "hub" {
		t.Errorf("FortNames: %v %v", names, err)
	}
	none, err := Layer("other").FortNames(root)
	if err != nil || none != nil {
		t.Errorf("FortNames of an uninstalled layer: %v %v", none, err)
	}
	core, err := CoreForts(root)
	if err != nil || strings.Join(core, ",") != "edge,hub" {
		t.Errorf("CoreForts: %v %v", core, err)
	}
	if _, err := CoreForts(t.TempDir()); err == nil {
		t.Error("CoreForts outside an estate: want an error")
	}
	// The core's estate, read loosely for the role's own entries.
	var c struct {
		Serves map[string][]struct {
			Fort  string
			Ports []int
		}
	}
	if err := CoreEstate(root, &c); err != nil || len(c.Serves["backups"]) != 1 || c.Serves["backups"][0].Ports[0] != 30900 {
		t.Errorf("CoreEstate: %+v %v", c, err)
	}
}

func TestRoot(t *testing.T) {
	root := estate(t)
	t.Setenv(RootEnv, root)
	if got, err := Root(); err != nil || got != root {
		t.Errorf("Root from env: %s %v", got, err)
	}
	t.Setenv(RootEnv, t.TempDir())
	if _, err := Root(); err == nil {
		t.Error("an env root with no estate.toml: want an error")
	}
	t.Setenv(RootEnv, "")
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(root, "forts"))
	if got, err := Root(); err != nil || got != root {
		t.Errorf("Root from the checkout: %s %v", got, err)
	}
}

func TestPlan(t *testing.T) {
	root := estate(t)
	p := &Plan{Root: root}
	p.New("layers/example/forts/edge.toml", "the fort's declaration", []byte("every = \"weekly\"\n"))
	p.New("layers/example/estate.toml", "", nil) // exists: present, never overwritten
	if err := p.Append("forts/hub.toml", "+ a line", func(s string) (string, error) { return s + "x = 1\n", nil }); err != nil {
		t.Fatal(err)
	}
	if err := p.Append("forts/edge.toml", "no-op", func(s string) (string, error) { return s, nil }); err != nil {
		t.Fatal(err)
	}
	p.Remove("layers/example/forts/hub.toml", "gone")
	p.Remove("layers/example/forts/none.toml", "")
	if len(p.Actions) != 3 || len(p.Present) != 3 {
		t.Fatalf("actions %d present %d", len(p.Actions), len(p.Present))
	}
	var out bytes.Buffer
	p.Report(&out, "fort-example scaffold edge", false)
	for _, want := range []string{"● layers/example/forts/edge.toml", "○ forts/hub.toml", "✗ layers/example/forts/hub.toml", "· layers/example/estate.toml", "Plan only"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, out.String())
		}
	}
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(root, "layers/example/forts/edge.toml")); string(raw) != "every = \"weekly\"\n" {
		t.Errorf("new file: %q", raw)
	}
	if raw, _ := os.ReadFile(filepath.Join(root, "forts/hub.toml")); !strings.HasSuffix(string(raw), "x = 1\n") {
		t.Errorf("appended file: %q", raw)
	}
	if _, err := os.Stat(filepath.Join(root, "layers/example/forts/hub.toml")); err == nil {
		t.Error("removed file is still there")
	}
	if raw, _ := os.ReadFile(filepath.Join(root, "layers/example/estate.toml")); !strings.Contains(string(raw), "store") {
		t.Error("a present file was overwritten")
	}
}

func TestReport(t *testing.T) {
	var out bytes.Buffer
	n := Report(&out, "hub", []Finding{{OK, "layers/x", "fine"}, {Warning, "a", "w"}, {Error, "b", "e"}, {"typo", "c", "lost?"}}, true)
	if n != 2 {
		t.Errorf("errors: %d", n)
	}
	for _, want := range []string{"== hub", "✓ layers/x", "! a: w", "::warning::a: w", "✗ b: e", "::error::b: e", "unknown finding level"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("lacks %q:\n%s", want, out.String())
		}
	}
}

func TestEditors(t *testing.T) {
	y, err := AppendListItem("resources:\n  - a.yaml\n\npatches: []\n", "resources", "b.yaml")
	if err != nil || y != "resources:\n  - a.yaml\n  - b.yaml\n\npatches: []\n" {
		t.Errorf("AppendListItem: %q %v", y, err)
	}
	// An emptied list reopens for the new entry (fort-layers#26).
	y, err = AppendListItem("kind: Kustomization\nresources: []\n", "resources", "a.yaml")
	if err != nil || y != "kind: Kustomization\nresources:\n  - a.yaml\n" {
		t.Errorf("AppendListItem on an empty list: %q %v", y, err)
	}
	// CollapseEmptyList: the last removed item leaves `resources: []`,
	// never a null list; a block with an item is untouched.
	if c := CollapseEmptyList("# a comment\nkind: Kustomization\nresources:\n", "resources"); c != "# a comment\nkind: Kustomization\nresources: []\n" {
		t.Errorf("CollapseEmptyList: %q", c)
	}
	for _, keep := range []string{"resources:\n  - a.yaml\n", "resources: []\n", "kind: Kustomization\n"} {
		if c := CollapseEmptyList(keep, "resources"); c != keep {
			t.Errorf("CollapseEmptyList changed %q to %q", keep, c)
		}
	}
	tm := TOMLSetInTable("mesh_ip = \"1\"\n\n[backups]\nstore = \"b2\"   # the store\n", "backups", "store", `"rustfs"`)
	if !strings.Contains(tm, "store = \"rustfs\"   # the store") {
		t.Errorf("TOMLSetInTable rewrite: %q", tm)
	}
	tm = TOMLSetInTable("mesh_ip = \"1\"\n", "backups", "store", `"b2"`)
	if !TOMLHasTable(tm, "backups") || !strings.HasSuffix(tm, "[backups]\nstore = \"b2\"\n") {
		t.Errorf("TOMLSetInTable append: %q", tm)
	}
	n, err := NixEnsureImport("{\n  imports = [\n    ./a.nix\n  ];\n}\n", "inputs.fort-layers.nixosModules.backups-pv-backup")
	if err != nil || !strings.Contains(n, "    inputs.fort-layers.nixosModules.backups-pv-backup\n  ];") {
		t.Errorf("NixEnsureImport: %q %v", n, err)
	}
	again, _ := NixEnsureImport(n, "inputs.fort-layers.nixosModules.backups-pv-backup  # twice")
	if again != n {
		t.Error("NixEnsureImport doubled a module")
	}
	b, err := NixEnsureBlock("{\n  a = 1;\n}\n", "sfi.backups", "  sfi.backups.enable = true;")
	if err != nil || b != "{\n  a = 1;\n\n  sfi.backups.enable = true;\n}\n" {
		t.Errorf("NixEnsureBlock: %q %v", b, err)
	}
}
