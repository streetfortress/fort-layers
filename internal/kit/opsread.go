package kit

import (
	"os"
	"path/filepath"
	"strings"
)

// The read-only ops identity's seam for a layer (sfi/forts#169): the
// core's ops-readonly.yaml carries an aggregated ClusterRole,
// ops-ro-layers-read, bound to kube-system/ops-ro, that the control
// plane fills from every ClusterRole labelled sfi.ops/read: "true". A
// layer ships a labelled ClusterRole and no binding, so it names no
// service account of the core's. The label is the fifth thing the core
// knows (forts' docs/layers.md).

// OpsReadLabel is the label on a layer's read rule.
const OpsReadLabel = `sfi.ops/read: "true"`

// OpsReadFile is the core's file, the estate's once init rendered it.
const OpsReadFile = "flux/infrastructure/configs/ops-readonly.yaml"

// OpsAggregatedRole is the core's aggregated role.
const OpsAggregatedRole = "ops-ro-layers-read"

// OpsAggregates says the estate's ops-readonly.yaml carries the
// aggregated role: without it a layer's labelled rule has no taker, and
// the ops identity reads none of the layer's kinds. An estate rendered
// before forts#169 adds the two objects by hand; init never rewrites.
func OpsAggregates(root string) bool {
	raw, err := os.ReadFile(filepath.Join(root, OpsReadFile))
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(l) == "name: "+OpsAggregatedRole {
			return true
		}
	}
	return false
}

// OpsMissingMsg is the finding a layer's check reports when the
// aggregated role is absent.
const OpsMissingMsg = "carries no ClusterRole " + OpsAggregatedRole + " — the layer's read rule is labelled " + OpsReadLabel + " and nothing aggregates it, so the ops identity reads none of the layer's kinds; the core's init template has the aggregated role and its binding (sfi/forts#169): add both to this file"
