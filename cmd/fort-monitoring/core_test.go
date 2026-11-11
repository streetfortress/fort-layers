package main

import (
	"testing"

	"forge.streetfortress.cloud/oss/fort-layers/internal/kit"
)

// A [cluster] table with or without keys is a cluster; no table is a
// plain fort. The core's manifest is read loosely: its other keys are
// not this role's.
func TestCoreFortCluster(t *testing.T) {
	root := t.TempDir()
	write(t, root, "forts/a.toml", "mesh_ip = \"10.9.0.2\"\n[cluster]\n")
	write(t, root, "forts/b.toml", "mesh_ip = \"10.9.0.3\"\n[cluster]\ndatastore = \"sqlite\"\naudit_logging = true\n[shape]\nvcpus = 4\n")
	write(t, root, "forts/c.toml", "mesh_ip = \"10.9.0.4\"\n[libvirt]\nip = \"10.0.0.4\"\n")
	for name, want := range map[string]bool{"a": true, "b": true, "c": false} {
		f, err := loadCoreFort(root, name)
		if err != nil || f.IsCluster() != want || f.MeshIP == "" {
			t.Errorf("%s: %+v %v", name, f, err)
		}
	}
}

func TestHubMeshIP(t *testing.T) {
	root := t.TempDir()
	write(t, root, "estate.toml", "name = \"test\"\n[source]\nrepo = \""+testRepo+"\"\n[hubs.monitoring]\nestate = \""+testRepo+"\"\nfort = \"hub\"\n")
	write(t, root, "forts/hub.toml", "mesh_ip = \"10.9.0.2\"\n[cluster]\n")
	core, err := loadCore(root)
	if err != nil {
		t.Fatal(err)
	}
	h, ok := core.hub()
	if !ok || !core.hubLocal(h) || !core.isHubFort("hub") || core.isHubFort("edge") {
		t.Fatalf("local hub: %+v %v", h, ok)
	}
	if ip, err := hubMeshIP(root, core, h); err != nil || ip != "10.9.0.2" {
		t.Errorf("local: %q %v", ip, err)
	}
	// A hub in another estate: its remotes entry, or a refusal by name.
	write(t, root, "estate.toml", "name = \"test\"\n[source]\nrepo = \""+testRepo+"\"\n[hubs.monitoring]\nestate = \""+remoteRepo+"\"\nfort = \"central\"\n")
	core, _ = loadCore(root)
	h, _ = core.hub()
	if core.hubLocal(h) || core.isHubFort("central") {
		t.Fatal("remote hub read as local")
	}
	if _, err := hubMeshIP(root, core, h); err == nil {
		t.Error("no peers.json: want an error")
	}
	write(t, root, "mesh/peers.json", `{"peers": {}, "remotes": {"central": {"wg_vpn_ip": "10.9.0.5"}}}`)
	if ip, err := hubMeshIP(root, core, h); err != nil || ip != "10.9.0.5" {
		t.Errorf("remote: %q %v", ip, err)
	}
	write(t, root, "mesh/peers.json", `{"peers": {}, "remotes": {}}`)
	if _, err := hubMeshIP(root, core, h); err == nil || !contains(err.Error(), "remotes.central") {
		t.Errorf("no remotes entry: %v", err)
	}
	_ = kit.RootEnv
}
