// fort-monitoring — the monitoring role of fort-layers: the edge stack
// every cluster fort runs (the core ships no observability opinion,
// forts#270), a hub that watches every fort of the estate, and the files
// each fort renders to be watched. The watched side: the fort's file for
// the hub (flux/hub/<hub>/<fort>.yaml), the hub's address in
// cluster-vars, the edge Prometheus's push and the log shipper. The
// hub's side: the central stack, its subscription, the gateway's
// listener, the host's ports, and the stack's credential. The design is
// docs/monitoring.md.
//
//	fort monitoring init [--no-metrics] [--no-logs]
//	fort monitoring scaffold <fort>
//	fort monitoring check [--all | <fort>]
//	fort monitoring mint <hub>
//	fort monitoring dismantle <fort>
package main

import (
	"fmt"
	"os"

	"forge.streetfortress.cloud/oss/fort-layers/internal/kit"
)

const role = kit.Layer("monitoring")

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "init":
		err = initCmd(os.Args[2:])
	case "scaffold":
		err = scaffoldCmd(os.Args[2:])
	case "check":
		err = check(os.Args[2:], os.Stdout)
	case "dismantle":
		err = dismantleCmd(os.Args[2:])
	case "mint":
		err = mintCmd(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		os.Exit(2)
	default:
		err = fmt.Errorf("unknown verb %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: fort monitoring <verb> [args]   (fort-monitoring when run by hand)

  Author verbs lay the files down, stage them, and print what they did
  in the marked-tree shape. The gate changes nothing. docs/monitoring.md.

  init [--no-metrics] [--no-logs]
      render layers/monitoring/estate.toml: what every cluster fort of
      this estate pushes to its monitoring hub — metrics (the edge
      Prometheus remote-writes, port 30990) and logs (fluent-bit ships
      to the hub's Loki, port 31100), both on unless a flag turns one
      off. The hub itself is the core's: [hubs.monitoring] in estate.toml.

  scaffold <fort>
      render what the fort needs. For a cluster fort, once per estate:
      the edge stack into flux/infrastructure/configs/monitoring/ (the
      edge Prometheus and Alertmanager, node-exporter's scrape seam, the
      rules, the mesh NodePorts, the ops read rule) and the three
      controllers (prometheus-operator, its CRDs, kube-state-metrics).
      With a hub named: the fort's file in flux/hub/<hub>/ (a cluster
      fort's dead man's switch, Loki rule and edge Alertmanager
      datasource; a plain fort's scrape target and HostDown), listed in
      that directory's kustomization; HUB_MESH_IP in the cluster's
      cluster-vars; and once per estate the push the [push] table
      declares — the remoteWrite patch on the edge Prometheus and the
      fluent-bit controller. Writes the fort's [monitoring] file when it
      is not there. On the fort that serves the role, from its
      [monitoring.hub] table: the central stack in
      flux/apps/<fort>/monitoring-central/, the subscription in serves/,
      a Certificate and a listener for grafana.<hostname>, the module
      that opens 30990 and 31100 on the mesh. Never overwrites; a re-run
      fills gaps. Then fort monitoring mint on the hub.

  check [--all | <fort>]
      the layer's gate: the edge stack is rendered and listed; every
      fort the core declares has its file and the hub's directory lists
      it; no file is there for a fort the core does not declare; the
      hub's directory builds; each cluster's HUB_MESH_IP is the hub's
      address on this mesh; the push the estate declares is rendered and
      listed. Exits nonzero on any error; annotates for CI when
      GITEA_ACTIONS is set.

  mint <hub>
      write the stack's credential, after scaffold: grafana-admin. A
      file that exists is kept. The Alertmanager's config is committed
      in the clear; a notifier is an opt-in edit (docs/monitoring.md).

  dismantle <fort>
      the layer's half of a retirement, BEFORE fort dismantle: remove the
      fort's file for the hub and its line, and the fort's [monitoring]
      file. The hub then drops the fort's rules by prune.`)
}
