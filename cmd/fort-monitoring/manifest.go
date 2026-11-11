package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// The two files of the monitoring layer in an estate (docs/monitoring.md).
//
// layers/monitoring/estate.toml holds what the layer decides for the
// estate. The hub is the core's ([hubs.monitoring] in estate.toml), so
// this file does not name it; it says what every cluster fort pushes.
//
//	[push]
//	metrics = true   # the edge Prometheus remote-writes to the hub (port 30990)
//	logs = true      # fluent-bit ships container and journal logs to the hub's Loki (port 31100)
//
// layers/monitoring/forts/<fort>.toml is required for every fort the
// core declares, once the core names a hub: [monitoring] with nothing
// under it is the positive declaration that the hub watches this fort.
// The hub fort — this estate's [hubs.monitoring] fort — adds
// [monitoring.hub], and the central stack renders from it:
//
//	[monitoring.hub]
//	hostname = "central.example.org"   # grafana.<hostname> on the fort's Gateway
//	[monitoring.hub.retention]         # the hub's history, on the fort's disk
//	metrics = "30d"
//	logs = "30d"

// Estate is layers/monitoring/estate.toml.
type Estate struct {
	Push *Push `toml:"push"`
}

// Push is the [push] table. Both default to on: an estate that names a
// hub pushes to it; a flag of init, or a line here, turns one off.
type Push struct {
	Metrics *bool `toml:"metrics"`
	Logs    *bool `toml:"logs"`
}

func (e Estate) PushMetrics() bool { return e.Push == nil || e.Push.Metrics == nil || *e.Push.Metrics }
func (e Estate) PushLogs() bool    { return e.Push == nil || e.Push.Logs == nil || *e.Push.Logs }

// Summary is the one line a report prints for the estate's push.
func (e Estate) Summary() string {
	var on []string
	if e.PushMetrics() {
		on = append(on, "metrics")
	}
	if e.PushLogs() {
		on = append(on, "logs")
	}
	if len(on) == 0 {
		return "push: nothing — the hub watches the forts and receives no stream"
	}
	return "push: " + strings.Join(on, ", ")
}

// Fort is layers/monitoring/forts/<fort>.toml.
type Fort struct {
	Monitoring *Monitoring `toml:"monitoring"`
}

// Monitoring is the [monitoring] table: empty for a watched fort, with
// the hub's table on the fort that serves the role.
type Monitoring struct {
	Hub *Hub `toml:"hub"`
}

// Hub is [monitoring.hub]: what the central stack renders from.
type Hub struct {
	// Hostname is the suffix of the name the stack serves on the fort's
	// Gateway: grafana.<hostname>. Required: no estate value derives it.
	Hostname  string     `toml:"hostname"`
	Retention *Retention `toml:"retention"`
}

// Retention is [monitoring.hub.retention]: how long the hub keeps what
// it receives, on the fort's disk — the central Prometheus's TSDB and
// Loki's filesystem store. A layer built on this one that adds an
// object store owns longer horizons.
type Retention struct {
	Metrics string `toml:"metrics"`
	Logs    string `toml:"logs"`
}

var (
	hostnameRE  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	retentionRE = regexp.MustCompile(`^[0-9]+[hdw]$`)
)

func (f Fort) Validate() error {
	if f.Monitoring == nil {
		return fmt.Errorf("no [monitoring] table — the positive declaration that the hub watches this fort (an empty table is enough)")
	}
	if h := f.Monitoring.Hub; h != nil {
		if !hostnameRE.MatchString(h.Hostname) {
			return fmt.Errorf("[monitoring.hub] hostname %q — want the DNS suffix the stack serves under (central.example.org: grafana.central.example.org)", h.Hostname)
		}
		for name, v := range map[string]string{"metrics": h.RetMetrics(), "logs": h.RetLogs()} {
			if !retentionRE.MatchString(v) {
				return fmt.Errorf("[monitoring.hub.retention] %s %q — want a duration in days, hours or weeks (30d)", name, v)
			}
		}
	}
	return nil
}

func (h *Hub) RetMetrics() string {
	return orDefault(h.Retention, func(r *Retention) string { return r.Metrics }, "30d")
}
func (h *Hub) RetLogs() string {
	return orDefault(h.Retention, func(r *Retention) string { return r.Logs }, "30d")
}

func orDefault(r *Retention, get func(*Retention) string, def string) string {
	if r == nil || get(r) == "" {
		return def
	}
	return get(r)
}

func loadEstate(root string) (Estate, error) {
	if !role.Installed(root) {
		return Estate{}, fmt.Errorf("no %s — fort monitoring init writes it (fort layer add runs it)", role.EstatePath())
	}
	var e Estate
	if err := role.LoadEstate(root, &e); err != nil {
		return Estate{}, err
	}
	return e, nil
}

func loadFort(root, fort string) (Fort, error) {
	var f Fort
	if err := role.LoadFort(root, fort, &f); err != nil {
		return Fort{}, err
	}
	if err := f.Validate(); err != nil {
		return Fort{}, fmt.Errorf("%s: %w", role.FortPath(fort), err)
	}
	return f, nil
}

// hubOf is the fort's [monitoring.hub], nil when the file is absent, does
// not parse, or carries none.
func hubOf(root, fort string) *Hub {
	f, err := loadFort(root, fort)
	if err != nil || f.Monitoring == nil {
		return nil
	}
	return f.Monitoring.Hub
}

// --- the paths the role renders to

// The fort's file for the hub lives in the fort's OWN estate, under the
// hub fort's name whichever estate holds it (forts#38): the hub
// subscribes to the directory with flux.
func hubDir(hub string) string            { return filepath.Join("flux/hub", hub) }
func hubKustomization(hub string) string  { return filepath.Join(hubDir(hub), "kustomization.yaml") }
func hubFilePath(hub, fort string) string { return filepath.Join(hubDir(hub), fort+".yaml") }

func clusterVarsPath(fort string) string {
	return filepath.Join("flux/clusters", fort, "cluster-vars.yaml")
}

// The hub fort's tree: the stack, the subscription, the gateway it adds
// a listener to, the host file it opens the ports in.
func appsDir(fort string) string          { return filepath.Join("flux/apps", fort) }
func stackDir(fort string) string         { return filepath.Join(appsDir(fort), stackName) }
func servesDir(fort string) string        { return filepath.Join(appsDir(fort), servesName) }
func gatewayDir(fort string) string       { return filepath.Join(appsDir(fort), "gateway") }
func hostNixPath(fort string) string      { return filepath.Join("nixos/hosts", fort, "default.nix") }
func servesPath(fort, name string) string { return filepath.Join(servesDir(fort), name+".yaml") }

// The estate-wide renders in the core's shared infrastructure: the edge
// stack (every cluster fort applies it), the push as a kustomize patch
// beside the edge Prometheus, and the log shipper as a controller.
const (
	monitoringConfigs = "flux/infrastructure/configs/monitoring"
	configsKust       = "flux/infrastructure/configs/kustomization.yaml"
	pushFile          = "push-metrics.yaml"
	controllersDir    = "flux/infrastructure/controllers"
	fluentBitName     = "fluent-bit"
	hubVar            = "HUB_MESH_IP"
	// The hub's namespace: apart from the edge stack's `monitoring`, so
	// the operator's per-namespace alertmanager-operated Service cannot
	// fan every edge alert into both.
	centralNS   = "monitoring-central"
	stackName   = "monitoring-central"
	servesName  = "serves"
	portMetrics = 30990
	portLogs    = 31100
)

var fluentBitDir = filepath.Join(controllersDir, fluentBitName)

// The edge stack's files under monitoringConfigs, in the kustomization's
// order; the kustomization itself renders from a template (the push's
// patch entry is conditional).
var edgeFiles = []struct{ name, note string }{
	{"prometheus-edge.yaml", "the edge Prometheus: 24h local, rules and monitors everywhere but scope central"},
	{"alertmanager-edge.yaml", "the edge Alertmanager: pages with the mesh down; its UI on mesh NodePort 30993"},
	{"alertmanager-config.yaml", "the committed config: a null receiver until the estate opts a notifier in"},
	{"servicemonitors.yaml", "kube-state-metrics, kubelet and cadvisor"},
	{"podmonitors.yaml", "traefik, flux, cert-manager"},
	{"node-exporter.yaml", "the host's node_exporter (NixOS, 9100 on the mesh), scraped through a headless Service"},
	{"rules-edge.yaml", "\"this cluster is unhealthy\": evaluates and pages with the mesh down"},
	{"mesh-services-edge.yaml", "NodePorts 30909 (Prometheus UI) and 30993 (Alertmanager UI, silences)"},
	{"ops-read.yaml", "the labelled read rule the core's ops-ro-layers-read aggregates (forts#169)"},
}

// The three controllers the edge stack needs, each a directory under
// controllersDir, with its files.
var edgeControllers = []struct {
	name  string
	note  string
	files []string
}{
	{"prometheus-operator-crds", "the operator's CRDs, their own release so the operator never owns them", []string{"kustomization.yaml", "namespace.yaml", "release.yaml"}},
	{"prometheus-operator", "kube-prometheus-stack with everything off but the operator", []string{"kustomization.yaml", "release.yaml"}},
	{"kube-state-metrics", "cluster state as metrics, flux CR status included", []string{"kustomization.yaml", "namespace.yaml", "repository.yaml", "release.yaml"}},
}

// The post-scaffold notes for the shapes (also what check reports).
const (
	clusterNote = "dead man's switch (metrics, logs), edge Alertmanager datasource"
	plainNote   = "mesh scrape target (node-exporter), HostDown"
	hubNote     = "the hub: the central stack, its subscription, the gateway, the ports"
	edgeNote    = "the edge stack; no hub to render for"
)
