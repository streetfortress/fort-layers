package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"forge.streetfortress.cloud/oss/fort-layers/internal/kit"
)

// What this role reads of the core's files, loosely (kit.CoreEstate,
// kit.Decode): the core's keys are the core's, and the role names only
// the few it needs. The hub is declared there (forts' docs/hub.md): the
// estate's [hubs.monitoring] names the hub fort and its estate — a fort
// of this estate, or one in another estate reached through a link. This
// role renders from that declaration and writes none of the core's
// files. The core's [[serves.<role>]] entries — a hub that serves OTHER
// estates — are not this layer's: a layer built on this one reads them
// and renders the cross-estate subscriptions.

// coreEstate is estate.toml, the keys this role reads.
type coreEstate struct {
	Name   string `toml:"name"`
	Source struct {
		Repo string `toml:"repo"`
	} `toml:"source"`
	Mesh struct {
		Zone string `toml:"zone"`
	} `toml:"mesh"`
	Hubs map[string]coreHub `toml:"hubs"`
}

// coreHub is one [hubs.<role>] table: the hub fort, by its estate's
// git URL and its name.
type coreHub struct {
	Estate string `toml:"estate"`
	Fort   string `toml:"fort"`
}

// coreFort is forts/<fort>.toml, the keys this role reads: the mesh
// address, and whether the fort is a cluster (a [cluster] table, with or
// without keys) or a plain host.
type coreFort struct {
	MeshIP  string         `toml:"mesh_ip"`
	Cluster map[string]any `toml:"cluster"`
}

// IsCluster says the manifest carries a [cluster] table: an edge
// Prometheus that pushes, flux that applies. A plain fort has neither,
// and the hub pulls node-exporter over the mesh instead.
func (f coreFort) IsCluster() bool { return f.Cluster != nil }

// The role's name in the core's tables.
const roleName = "monitoring"

func loadCore(root string) (coreEstate, error) {
	var e coreEstate
	if err := kit.CoreEstate(root, &e); err != nil {
		return e, err
	}
	return e, nil
}

func loadCoreFort(root, fort string) (coreFort, error) {
	var f coreFort
	if err := kit.Decode(filepath.Join(root, kit.CoreFortsDir, fort+".toml"), &f); err != nil {
		return f, err
	}
	return f, nil
}

// hub is the estate's [hubs.monitoring], if it names one.
func (e coreEstate) hub() (coreHub, bool) {
	h, ok := e.Hubs[roleName]
	return h, ok && h.Fort != ""
}

// hubLocal says the hub is a fort of this estate: its estate URL names
// this repo.
func (e coreEstate) hubLocal(h coreHub) bool { return sameRepo(h.Estate, e.Source.Repo) }

// zone is the estate's mesh zone (forts' docs/mesh.md): [mesh] zone, or
// <name>.mesh — the suffix of every fort's name on the mesh.
func (e coreEstate) zone() string {
	if e.Mesh.Zone != "" {
		return e.Mesh.Zone
	}
	return e.Name + ".mesh"
}

// isHubFort says the fort serves the role: it is this estate's own hub.
func (e coreEstate) isHubFort(fort string) bool {
	h, ok := e.hub()
	return ok && e.hubLocal(h) && h.Fort == fort
}

// hubMeshIP is where this estate's edges push: the monitoring hub's
// address on THIS mesh (forts#38). A hub that is a fort of this estate
// has it in its manifest (mesh_ip); a hub in another estate is on this
// mesh through a link, and its address here is its remotes entry in
// mesh/peers.json (forts' docs/mesh.md, "Linking"). A missing entry is
// refused by name, not rendered empty.
func hubMeshIP(root string, e coreEstate, h coreHub) (string, error) {
	if e.hubLocal(h) {
		f, err := loadCoreFort(root, h.Fort)
		if err != nil {
			return "", fmt.Errorf("estate.toml [hubs.monitoring] names %q as a fort of this estate: %w", h.Fort, err)
		}
		if f.MeshIP == "" {
			return "", fmt.Errorf("%s/%s.toml: no mesh_ip — the hub's address on this mesh", kit.CoreFortsDir, h.Fort)
		}
		return f.MeshIP, nil
	}
	const peersPath = "mesh/peers.json"
	raw, err := os.ReadFile(filepath.Join(root, peersPath))
	if err != nil {
		return "", fmt.Errorf("estate.toml [hubs.monitoring] names %s in %s, whose address on this mesh is its remotes entry: %w", h.Fort, ref(h.Estate), err)
	}
	var peers struct {
		Remotes map[string]struct {
			VPNIP string `json:"wg_vpn_ip"`
		} `json:"remotes"`
	}
	if err := json.Unmarshal(raw, &peers); err != nil {
		return "", fmt.Errorf("%s: %w", peersPath, err)
	}
	r, ok := peers.Remotes[h.Fort]
	if !ok || r.VPNIP == "" {
		return "", fmt.Errorf("estate.toml [hubs.monitoring] names %s in %s, and %s has no remotes.%s — the hub reaches this mesh through a link, and its address here is that entry (forts' docs/mesh.md, \"Linking\"); fort link add first", h.Fort, ref(h.Estate), peersPath, h.Fort)
	}
	return r.VPNIP, nil
}

// --- repo URLs: the kit's helpers, under the names this role's code uses

func ref(repo string) string       { return kit.Ref(repo) }
func shortName(repo string) string { return kit.ShortName(repo) }
func sameRepo(a, b string) bool    { return kit.SameRepo(a, b) }
