package kit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// The core's hub declaration, read loosely: a role that renders for a
// hub (the monitoring role) or beside one (the backups role's rule in
// the hub's directory) needs the hub fort's name and whether it is a
// fort of this estate, and nothing else of the table (forts' docs/hub.md).

// CoreHub is one [hubs.<role>] table of estate.toml.
type CoreHub struct {
	Estate string `toml:"estate"`
	Fort   string `toml:"fort"`
}

type coreHubs struct {
	Name   string `toml:"name"`
	Source struct {
		Repo string `toml:"repo"`
	} `toml:"source"`
	Hubs map[string]CoreHub `toml:"hubs"`
}

// CoreHubOf reads the estate's [hubs.<role>]: the hub, whether it is a
// fort of this estate (its estate URL names this repo), and whether the
// table is there at all.
func CoreHubOf(root, role string) (hub CoreHub, local, ok bool, err error) {
	var e coreHubs
	if err = CoreEstate(root, &e); err != nil {
		return
	}
	h, found := e.Hubs[role]
	if !found || h.Fort == "" {
		return
	}
	return h, SameRepo(h.Estate, e.Source.Repo), true, nil
}

// CoreName is the estate's name (estate.toml `name`): the value of the
// `estate` label every metric and log line leaves a fort with (forts#50).
func CoreName(root string) (string, error) {
	var e coreHubs
	if err := CoreEstate(root, &e); err != nil {
		return "", err
	}
	return e.Name, nil
}

// HubHost is the hub's name on THIS mesh, which a consumer dials:
// <fort>.<zone>, where the zone is this estate's — a hub that is a fort
// of this estate is a peer under it, and a hub in another estate is a
// remote under the same zone, through its link (forts#45). The name
// resolves on every fort (mesh-dns.nix) and in every cluster (the
// coredns-custom block), so no address is rendered and a re-addressed
// hub changes no file. A remote hub with no remotes entry is refused by
// name: the name would resolve to nothing.
func HubHost(root, role, zone string, h CoreHub, local bool) (string, error) {
	table := "[hubs." + role + "]"
	if local {
		if err := Decode(filepath.Join(root, CoreFortsDir, h.Fort+".toml"), &struct{}{}); err != nil {
			return "", fmt.Errorf("estate.toml %s names %q as a fort of this estate: %w", table, h.Fort, err)
		}
		return h.Fort + "." + zone, nil
	}
	const peersPath = "mesh/peers.json"
	raw, err := os.ReadFile(filepath.Join(root, peersPath))
	if err != nil {
		return "", fmt.Errorf("estate.toml %s names %s in %s, whose address on this mesh is its remotes entry: %w", table, h.Fort, Ref(h.Estate), err)
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
		return "", fmt.Errorf("estate.toml %s names %s in %s, and %s has no remotes.%s — the hub reaches this mesh through a link, and its name here resolves to that entry (forts' docs/mesh.md, \"Linking\"); fort link add on the hub, fort estate link here, first", table, h.Fort, Ref(h.Estate), peersPath, h.Fort)
	}
	return h.Fort + "." + zone, nil
}
