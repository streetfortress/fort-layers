package kit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyInventory(t *testing.T) {
	root := t.TempDir()
	touch := func(name string) {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, old := LegacyInventory(root, "."); old {
		t.Error("an empty directory is not legacy")
	}
	touch(LegacyInventoryFile)
	msg, old := LegacyInventory(root, ".")
	if !old || msg != "rename the file forts.contract.yaml to forts.inventory.yaml (oss/forts#88)" {
		t.Errorf("legacy: %v %q", old, msg)
	}
	touch(InventoryFile)
	if _, old := LegacyInventory(root, "."); old {
		t.Error("the new name beside the old is not a rename")
	}
}
