package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forge.streetfortress.cloud/oss/fort-layers/internal/kit"
)

const (
	testRepo   = "ssh://git@forge.example:30022/oss/test"
	remoteRepo = "ssh://git@forge.example:30022/oss/estate-hub"
)

// estate lays down the core's render of an estate with three forts —
// `hub`, this estate's own monitoring hub; `edge`, a second cluster;
// `plain`, a host with no [cluster] — the way fort init and fort
// scaffold leave it, plus the Gateway battery the hub's render appends
// to, and installs the layer with both streams on.
func estate(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for path, text := range map[string]string{
		"estate.toml":                          "name = \"test\"\n[source]\nrepo = \"" + testRepo + "\"\n[hubs.monitoring]\nestate = \"" + testRepo + "\"\nfort = \"hub\"\n",
		"forts/hub.toml":                       "mesh_ip = \"10.9.0.2\"\n[cluster]\n",
		"forts/edge.toml":                      "mesh_ip = \"10.9.0.3\"\n[cluster]\n",
		"forts/plain.toml":                     "mesh_ip = \"10.9.0.4\"\n[libvirt]\nip = \"10.0.0.4\"\n",
		"nixos/flake.nix":                      "{\n  outputs = inputs@{ ... }:\n    let\n      layers = sys: {\n        fort-monitoring = inputs.fort-layers.packages.${sys}.fort-monitoring;\n      };\n    in { };\n}\n",
		"nixos/hosts/hub/default.nix":          "{ inputs, config, ... }:\n\n{\n  imports = [\n    inputs.forts.nixosModules.k3s-server\n  ];\n\n  networking.hostName = \"hub\";\n\n  system.stateVersion = \"25.11\";\n}\n",
		"flux/clusters/hub/cluster-vars.yaml":  "# the estate's comment\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cluster-vars\n  namespace: flux-system\ndata:\n  CLUSTER_NAME: hub\n  ESTATE_NAME: test\n  MESH_IP: 10.9.0.2\n",
		"flux/clusters/edge/cluster-vars.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cluster-vars\n  namespace: flux-system\ndata:\n  CLUSTER_NAME: edge\n  ESTATE_NAME: test\n  MESH_IP: 10.9.0.3\n",
		// The core's post-forts#270 shared layer: no monitoring entry, no
		// observability controllers — this layer appends its own.
		"flux/infrastructure/configs/kustomization.yaml":     "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n  - cluster-roles.yaml\n  - ops-readonly.yaml\n",
		"flux/infrastructure/configs/ops-readonly.yaml":      "apiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRole\nmetadata:\n  name: ops-ro-layers-read\naggregationRule:\n  clusterRoleSelectors:\n    - matchLabels:\n        sfi.ops/read: \"true\"\nrules: []\n",
		"flux/infrastructure/controllers/kustomization.yaml": "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n  - gateway-api\n  - traefik\n  - cert-manager\n",
		"flux/apps/hub/kustomization.yaml":                   "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n  - sops-canary.yaml\n  - gateway\n",
		"flux/apps/hub/gateway/kustomization.yaml":           "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n  - gateway.yaml\n  - certificate-forgejo.yaml\npatches:\n  - path: listener-forgejo.yaml\n    target:\n      group: gateway.networking.k8s.io\n      version: v1\n      kind: Gateway\n      name: homelab\n      namespace: traefik\n",
		"flux/apps/hub/gateway/certificate-forgejo.yaml":     "apiVersion: cert-manager.io/v1\nkind: Certificate\nmetadata:\n  name: forgejo\n  namespace: traefik\nspec:\n  secretName: forgejo-tls\n  dnsNames:\n    - forgejo.hub.lofi\n  issuerRef:\n    kind: ClusterIssuer\n    name: step-ca\n",
		"layers/monitoring/estate.toml":                      "[push]\nmetrics = true\nlogs = true\n",
	} {
		write(t, root, path, text)
	}
	t.Setenv(kit.RootEnv, root)
	return root
}

// hubFile is the refined hub table an operator writes after the first
// scaffold: the lab's hostname and a shorter metric horizon.
const hubFile = "[monitoring]\n[monitoring.hub]\nhostname = \"hub.lofi\"\n[monitoring.hub.retention]\nmetrics = \"14d\"\n"

func write(t *testing.T, root, path, text string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, path string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func contains(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

func apply(t *testing.T, p *kit.Plan) {
	t.Helper()
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
}

// scaffoldAll renders the three forts, the hub in its two steps.
func scaffoldAll(t *testing.T, root string) {
	t.Helper()
	for _, f := range []string{"hub", "edge", "plain"} {
		p, _, err := planScaffold(root, f)
		if err != nil {
			t.Fatal(f, err)
		}
		apply(t, p)
	}
	write(t, root, role.FortPath("hub"), hubFile)
	p, _, err := planScaffold(root, "hub")
	if err != nil {
		t.Fatal(err)
	}
	apply(t, p)
}

func gate(t *testing.T) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := check([]string{"--all"}, &out)
	return out.String(), err
}

func TestScaffoldRendersTheWatchedSide(t *testing.T) {
	root := estate(t)

	// The hub fort first: its file with the hub's shape — and the run
	// stops there for the stack — the edge stack and the push once per
	// estate, the fort's own hub file, the hub's address in its
	// cluster-vars.
	p, note, err := planScaffold(root, "hub")
	if err != nil {
		t.Fatal(err)
	}
	if note != clusterNote {
		t.Errorf("note: %q", note)
	}
	apply(t, p)
	for _, a := range p.Actions {
		if strings.HasPrefix(a.Path, stackDir("hub")) || strings.HasPrefix(a.Path, servesDir("hub")) {
			t.Errorf("the stack rendered before the table was refined: %s", a.Path)
		}
	}
	if ft := read(t, root, role.FortPath("hub")); !contains(ft, "[monitoring]\n", "[monitoring.hub]", "hostname = \"hub.test.mesh\"", "this estate's [hubs.monitoring]", "metrics = \"30d\"") {
		t.Errorf("hub fort file:\n%s", ft)
	}
	// The edge stack, in the shared layer.
	if s := read(t, root, filepath.Join(monitoringConfigs, "prometheus-edge.yaml")); !contains(s, "name: edge", "retention: 24h", "cluster: ${CLUSTER_NAME}") {
		t.Errorf("edge prometheus:\n%s", s)
	}
	if s := read(t, root, filepath.Join(monitoringConfigs, "alertmanager-config.yaml")); !contains(s, "name: alertmanager-edge", "receiver: none", "url_file: /etc/alertmanager/secrets/alertmanager-webhook/url") {
		t.Errorf("edge alertmanager config:\n%s", s)
	}
	if s := read(t, root, filepath.Join(monitoringConfigs, "ops-read.yaml")); !contains(s, `sfi.ops/read: "true"`, "monitoring.coreos.com") {
		t.Errorf("ops read rule:\n%s", s)
	}
	if s := read(t, root, filepath.Join(monitoringConfigs, "kustomization.yaml")); !contains(s, "  - ops-read.yaml\n", "patches:", "path: push-metrics.yaml") {
		t.Errorf("edge kustomization:\n%s", s)
	}
	if s := read(t, root, configsKust); !contains(s, "  - monitoring\n") {
		t.Errorf("configs kustomization:\n%s", s)
	}
	if s := read(t, root, filepath.Join(controllersDir, "prometheus-operator/release.yaml")); !contains(s, "kube-prometheus-stack") {
		t.Errorf("operator release:\n%s", s)
	}
	if k := read(t, root, filepath.Join(controllersDir, "kustomization.yaml")); !strings.HasSuffix(k, "  - prometheus-operator-crds\n  - prometheus-operator\n  - kube-state-metrics\n  - fluent-bit\n") {
		t.Errorf("controllers kustomization:\n%s", k)
	}
	// The fort's own renders.
	hubFileText := read(t, root, hubFilePath("hub", "hub"))
	if !contains(hubFileText, `absent(up{cluster="hub"})`, "estate: test", "alertmanager-operated.monitoring.svc.cluster.local:9093", "loki-rules-hub") {
		t.Errorf("hub's own file:\n%s", hubFileText)
	}
	if k := read(t, root, hubKustomization("hub")); !contains(k, "  - hub.yaml\n", "except through the hub's subscription") {
		t.Errorf("kustomization:\n%s", k)
	}
	if v := read(t, root, clusterVarsPath("hub")); !contains(v, "  HUB_MESH_IP: 10.9.0.2\n", "# the estate's comment") {
		t.Errorf("cluster-vars:\n%s", v)
	}
	if patch := read(t, root, filepath.Join(monitoringConfigs, pushFile)); !contains(patch, "kind: Prometheus", "http://${HUB_MESH_IP}:30990/api/v1/write") {
		t.Errorf("patch:\n%s", patch)
	}
	if r := read(t, root, filepath.Join(fluentBitDir, "release.yaml")); !contains(r, "Host ${HUB_MESH_IP}", "Port 31100", "estate=${ESTATE_NAME}") {
		t.Errorf("fluent-bit:\n%s", r)
	}

	// The second cluster: its datasource is over the mesh; the shared
	// renders are already there, so only the fort's own files are planned.
	p, _, err = planScaffold(root, "edge")
	if err != nil {
		t.Fatal(err)
	}
	apply(t, p)
	if f := read(t, root, hubFilePath("hub", "edge")); !contains(f, "http://10.9.0.3:30993", `absent(up{cluster="edge"})`) {
		t.Errorf("edge file:\n%s", f)
	}
	for _, a := range p.Actions {
		if strings.HasPrefix(a.Path, "flux/infrastructure/") {
			t.Errorf("a shared render was planned twice: %s", a.Path)
		}
	}
	if ft := read(t, root, role.FortPath("edge")); strings.Contains(ft, "[monitoring.hub]") {
		t.Error("a watched fort got the hub's shape")
	}

	// The plain fort: the pull shape, no cluster-vars, no edge stack of
	// its own to plan.
	p, note, err = planScaffold(root, "plain")
	if err != nil {
		t.Fatal(err)
	}
	if note != plainNote {
		t.Errorf("note: %q", note)
	}
	apply(t, p)
	if f := read(t, root, hubFilePath("hub", "plain")); !contains(f, "kind: ScrapeConfig", `targets: ["10.9.0.4:9100"]`, "jobName: host-node-exporter", "alert: HostDown", "estate: test") || strings.Contains(f, "grafana_datasource") {
		t.Errorf("plain file:\n%s", f)
	}
	// A re-run of the watched forts finds everything as planned.
	for _, f := range []string{"edge", "plain"} {
		p, _, err := planScaffold(root, f)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Actions) != 0 {
			t.Errorf("%s re-run: %+v", f, p.Actions)
		}
	}
}

func TestScaffoldRendersTheHub(t *testing.T) {
	root := estate(t)
	scaffoldAll(t, root)
	p, note, err := planScaffold(root, "hub")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Actions) != 0 {
		t.Errorf("re-run: %+v", p.Actions)
	}
	if note != hubNote {
		t.Errorf("note: %q", note)
	}
	// The stack, from the refined table.
	for _, f := range stackFiles {
		if !kit.Exists(root, filepath.Join(stackDir("hub"), f.name)) {
			t.Errorf("stack lacks %s", f.name)
		}
	}
	if s := read(t, root, filepath.Join(stackDir("hub"), "central-prometheus.yaml")); !contains(s, "retention: 14d", "enableRemoteWriteReceiver: true", "monitoring.sfi/scope: central", "name: alertmanager-operated") || strings.Contains(s, "thanos") {
		t.Errorf("central prometheus:\n%s", s)
	}
	if s := read(t, root, filepath.Join(stackDir("hub"), "loki.yaml")); !contains(s, "type: filesystem", "object_store: filesystem", "retention_period: 30d", "clusterLabelOverride: ${CLUSTER_NAME}") || strings.Contains(s, "s3") {
		t.Errorf("loki:\n%s", s)
	}
	if s := read(t, root, filepath.Join(stackDir("hub"), "grafana.yaml")); !contains(s, "root_url: https://grafana.hub.lofi", "- grafana.hub.lofi", "uid: fenqsyb8rfke8c", "prometheus-operated.monitoring-central") {
		t.Errorf("grafana:\n%s", s)
	}
	for _, d := range []string{"estate-overview", "fort-overview"} {
		if s := read(t, root, filepath.Join(stackDir("hub"), "dashboards", d+".json")); !contains(s, `"uid": "`+d+`"`, "fenqsyb8rfke8c") {
			t.Errorf("dashboard %s:\n%s", d, s)
		}
		if s := read(t, root, filepath.Join(stackDir("hub"), "kustomization.yaml")); !contains(s, "dashboards/"+d+".json", "grafana_dashboard:") {
			t.Errorf("stack kustomization lacks dashboard %s:\n%s", d, s)
		}
	}
	if s := read(t, root, filepath.Join(stackDir("hub"), "alertmanager-config.yaml")); !contains(s, "name: alertmanager-central", "receiver: none", "flux/apps/hub/monitoring-central/alertmanager-webhook.sops.yaml") {
		t.Errorf("central alertmanager config:\n%s", s)
	}
	if s := read(t, root, filepath.Join(stackDir("hub"), "kustomization.yaml")); !contains(s, "  - grafana-admin.sops.yaml\n", "  - alertmanager-config.yaml\n") || contains(s, "thanos") || contains(s, "discord") {
		t.Errorf("stack kustomization:\n%s", s)
	}
	if s := read(t, root, filepath.Join(stackDir("hub"), "forts.inventory.yaml")); !contains(s, "admin-password: {generate: hex24}") || contains(s, "alertmanager-central-config") {
		t.Errorf("contract:\n%s", s)
	}
	// The subscription: the own estate through flux-system.
	if s := read(t, root, servesPath("hub", "test")); !contains(s, "name: flux-system", "path: ./flux/hub/hub", "name: hub-test") || strings.Contains(s, "url: ") {
		t.Errorf("own subscription:\n%s", s)
	}
	if s := read(t, root, filepath.Join(servesDir("hub"), "kustomization.yaml")); !contains(s, "  - test.yaml\n") {
		t.Errorf("serves kustomization:\n%s", s)
	}
	if s := read(t, root, filepath.Join(appsDir("hub"), "kustomization.yaml")); !strings.HasSuffix(s, "  - gateway\n  - monitoring-central\n  - serves\n") {
		t.Errorf("apps kustomization:\n%s", s)
	}
	// The gateway: grafana only (no query tier in this layer), the
	// issuer the other certificate names.
	gw := gatewayDir("hub")
	if s := read(t, root, filepath.Join(gw, "certificate-grafana.yaml")); !contains(s, "- grafana.hub.lofi", "name: step-ca", "secretName: grafana-tls") {
		t.Errorf("certificate:\n%s", s)
	}
	if kit.Exists(root, filepath.Join(gw, "certificate-query.yaml")) {
		t.Error("a query certificate without a query tier")
	}
	if s := read(t, root, filepath.Join(gw, "kustomization.yaml")); !contains(s, "  - certificate-forgejo.yaml\n  - certificate-grafana.yaml\n", "  - path: listener-forgejo.yaml\n", "  - path: listener-grafana.yaml\n") || strings.Count(s, "patches:") != 1 {
		t.Errorf("gateway kustomization:\n%s", s)
	}
	// The host: the module and the option.
	if s := read(t, root, hostNixPath("hub")); !contains(s, "    inputs.fort-layers.nixosModules.monitoring-hub  #", "  sfi.monitoring-hub.enable = true;\n}\n") {
		t.Errorf("host:\n%s", s)
	}
	// The gate: no error; the credential is the warning mint clears.
	s, err := gate(t)
	if err != nil || strings.Contains(s, "✗") || !contains(s, "the edge stack is rendered", "the central stack at", "grafana.hub.lofi", "certificate and listener", "opens 30990 and 31100", "fort monitoring mint hub") {
		t.Errorf("check --all: %v\n%s", err, s)
	}
}

// An estate with no hub still gets the edge stack: each cluster watches
// itself, alerts land in the edge Alertmanager, and the gate is green
// with no hub-shaped render asked for.
func TestScaffoldWithoutAHub(t *testing.T) {
	root := estate(t)
	write(t, root, "estate.toml", "name = \"test\"\n[source]\nrepo = \""+testRepo+"\"\n")
	p, note, err := planScaffold(root, "edge")
	if err != nil {
		t.Fatal(err)
	}
	if note != edgeNote {
		t.Errorf("note: %q", note)
	}
	apply(t, p)
	if !kit.Exists(root, filepath.Join(monitoringConfigs, "prometheus-edge.yaml")) {
		t.Error("no edge stack")
	}
	if s := read(t, root, filepath.Join(monitoringConfigs, "kustomization.yaml")); strings.Contains(s, "push-metrics") {
		t.Errorf("a push with no hub:\n%s", s)
	}
	if kit.Exists(root, role.FortPath("edge")) || kit.Exists(root, hubDir("hub")) {
		t.Error("a hub-shaped render with no hub")
	}
	if s, err := gate(t); err != nil || strings.Contains(s, "✗") || !contains(s, "hub   none", "the edge Alertmanager is the only alerting", "the edge stack is rendered") {
		t.Errorf("check --all: %v\n%s", err, s)
	}
}

func TestScaffoldRefusals(t *testing.T) {
	root := estate(t)
	for name, want := range map[string]string{"nope": "declares no such fort", "Bad!": "not a fort name"} {
		if _, _, err := planScaffold(root, name); err == nil || !contains(err.Error(), want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A hub fort whose file lacks the table; a watched fort that carries
	// it.
	write(t, root, role.FortPath("hub"), "[monitoring]\n")
	if _, _, err := planScaffold(root, "hub"); err == nil || !contains(err.Error(), "has no [monitoring.hub]") {
		t.Errorf("hub without table: %v", err)
	}
	write(t, root, role.FortPath("edge"), hubFile)
	if _, _, err := planScaffold(root, "edge"); err == nil || !contains(err.Error(), "does not name edge its [hubs.monitoring]") {
		t.Errorf("edge with table: %v", err)
	}
	// A bad hostname, a bad retention.
	write(t, root, role.FortPath("hub"), "[monitoring]\n[monitoring.hub]\nhostname = \"Hub\"\n")
	if _, _, err := planScaffold(root, "hub"); err == nil || !contains(err.Error(), "hostname") {
		t.Errorf("hostname: %v", err)
	}
	write(t, root, role.FortPath("hub"), "[monitoring]\n[monitoring.hub]\nhostname = \"hub.lofi\"\n[monitoring.hub.retention]\nmetrics = \"soon\"\n")
	if _, _, err := planScaffold(root, "hub"); err == nil || !contains(err.Error(), "retention") {
		t.Errorf("retention: %v", err)
	}
	os.Remove(filepath.Join(root, role.FortPath("hub")))
	os.Remove(filepath.Join(root, role.FortPath("edge")))
	// The core's shared layer is not there: the layer has nothing to
	// append its stack to.
	os.Remove(filepath.Join(root, configsKust))
	if _, _, err := planScaffold(root, "edge"); err == nil || !contains(err.Error(), "the shared configs") {
		t.Errorf("no configs kustomization: %v", err)
	}
	write(t, root, configsKust, "resources:\n  - ops-readonly.yaml\n")
	// The layer not installed.
	os.Remove(filepath.Join(root, role.EstatePath()))
	if _, _, err := planScaffold(root, "edge"); err == nil || !contains(err.Error(), "fort monitoring init") {
		t.Errorf("not installed: %v", err)
	}
}

// An estate that pushes nothing renders no patch and no shipper, and the
// gate asks for none.
func TestScaffoldWithPushOff(t *testing.T) {
	root := estate(t)
	write(t, root, role.EstatePath(), "[push]\nmetrics = false\nlogs = false\n")
	p, _, err := planScaffold(root, "edge")
	if err != nil {
		t.Fatal(err)
	}
	apply(t, p)
	if kit.Exists(root, filepath.Join(monitoringConfigs, pushFile)) || kit.Exists(root, fluentBitDir) {
		t.Error("push rendered with [push] off")
	}
	if s := read(t, root, filepath.Join(monitoringConfigs, "kustomization.yaml")); strings.Contains(s, "push-metrics") {
		t.Errorf("the patch entry with [push] off:\n%s", s)
	}
	s, err := gate(t)
	if err == nil || !contains(s, "push  nothing", "✗ "+hubFilePath("hub", "hub")) || contains(s, pushFile) || contains(s, "fluent-bit") {
		t.Errorf("check --all: %v\n%s", err, s)
	}
}

func TestCheckFindsEveryGap(t *testing.T) {
	root := estate(t)
	// Nothing scaffolded: the edge stack is missing; every fort is
	// missing its file and its declaration; the push is missing; the hub
	// has no table.
	s, err := gate(t)
	if err == nil || !contains(s, "✗ "+monitoringConfigs, "lacks prometheus-edge.yaml", "✗ "+role.FortPath("hub"), "✗ "+hubFilePath("hub", "edge"), "✗ "+hubFilePath("hub", "plain"), "✗ "+filepath.Join(monitoringConfigs, pushFile), "✗ "+fluentBitDir, "plain fort the hub does not scrape", "flux cluster with no dead man's switch", "no [monitoring.hub]") {
		t.Errorf("empty:\n%s", s)
	}
	scaffoldAll(t, root)
	if s, err := gate(t); err != nil {
		t.Fatalf("clean: %v\n%s", err, s)
	}
	// An edge stack gap: a file gone, the directory unlisted, a
	// controller unlisted, the aggregated role gone.
	os.Remove(filepath.Join(root, monitoringConfigs, "ops-read.yaml"))
	if s, err := gate(t); err == nil || !contains(s, "lacks ops-read.yaml") {
		t.Errorf("edge gap:\n%s", s)
	}
	p, _, err := planScaffold(root, "edge")
	if err != nil {
		t.Fatal(err)
	}
	apply(t, p) // the re-run fills the gap
	cfg := read(t, root, configsKust)
	write(t, root, configsKust, strings.Replace(cfg, "  - monitoring\n", "", 1))
	if s, err := gate(t); err == nil || !contains(s, "does not list monitoring") {
		t.Errorf("unlisted stack:\n%s", s)
	}
	write(t, root, configsKust, cfg)
	ctrl := read(t, root, filepath.Join(controllersDir, "kustomization.yaml"))
	write(t, root, filepath.Join(controllersDir, "kustomization.yaml"), strings.Replace(ctrl, "  - kube-state-metrics\n", "", 1))
	if s, err := gate(t); err == nil || !contains(s, "does not list kube-state-metrics") {
		t.Errorf("unlisted controller:\n%s", s)
	}
	write(t, root, filepath.Join(controllersDir, "kustomization.yaml"), ctrl)
	ops := read(t, root, kit.OpsReadFile)
	write(t, root, kit.OpsReadFile, strings.Replace(ops, "ops-ro-layers-read", "something-else", 1))
	if s, err := gate(t); err == nil || !contains(s, "carries no ClusterRole ops-ro-layers-read") {
		t.Errorf("no aggregation:\n%s", s)
	}
	write(t, root, kit.OpsReadFile, ops)
	// Unlisted: the file exists and the hub never applies it.
	write(t, root, hubKustomization("hub"), "resources:\n  - hub.yaml\n  - plain.yaml\n")
	if s, err := gate(t); err == nil || !contains(s, "does not list edge.yaml") {
		t.Errorf("unlisted:\n%s", s)
	}
	write(t, root, hubKustomization("hub"), "resources:\n  - hub.yaml\n  - edge.yaml\n  - plain.yaml\n")
	// A wrong HUB_MESH_IP: the edge pushes into nothing.
	write(t, root, clusterVarsPath("edge"), "data:\n  CLUSTER_NAME: edge\n  HUB_MESH_IP: 10.9.0.9\n")
	if s, err := gate(t); err == nil || !contains(s, "HUB_MESH_IP is 10.9.0.9, and the hub's address on this mesh is 10.9.0.2") {
		t.Errorf("wrong ip:\n%s", s)
	}
	write(t, root, clusterVarsPath("edge"), "data:\n  CLUSTER_NAME: edge\n  HUB_MESH_IP: 10.9.0.2\n")
	// An orphan: a file for a fort with no manifest.
	write(t, root, hubFilePath("hub", "gone"), "kind: PrometheusRule\n")
	if s, err := gate(t); err == nil || !contains(s, "✗ "+hubFilePath("hub", "gone"), "fort monitoring dismantle gone") {
		t.Errorf("orphan:\n%s", s)
	}
	os.Remove(filepath.Join(root, hubFilePath("hub", "gone")))
	// Another role's file beside a fort's — <fort>-<role>.yaml — is not
	// an orphan.
	write(t, root, hubFilePath("hub", "edge-backups"), "kind: PrometheusRule\n")
	if s, err := gate(t); err != nil {
		t.Errorf("another role's file flagged: %v\n%s", err, s)
	}
	os.Remove(filepath.Join(root, hubFilePath("hub", "edge-backups")))
	// A stray declaration.
	write(t, root, role.FortPath("gone"), "[monitoring]\n")
	if s, err := gate(t); err == nil || !contains(s, "✗ "+role.FortPath("gone"), "a stray") {
		t.Errorf("stray:\n%s", s)
	}
	os.Remove(filepath.Join(root, role.FortPath("gone")))
	// The hub's gaps: the host without the module, the table on a
	// watched fort.
	host := read(t, root, hostNixPath("hub"))
	write(t, root, hostNixPath("hub"), strings.Replace(host, "  sfi.monitoring-hub.enable = true;\n", "", 1))
	if s, err := gate(t); err == nil || !contains(s, "sets no sfi.monitoring-hub") {
		t.Errorf("host:\n%s", s)
	}
	write(t, root, hostNixPath("hub"), host)
	write(t, root, role.FortPath("edge"), hubFile)
	if s, err := gate(t); err == nil || !contains(s, "✗ "+role.FortPath("edge"), "[monitoring.hub] on a fort estate.toml does not name") {
		t.Errorf("table on a watched fort:\n%s", s)
	}
	write(t, root, role.FortPath("edge"), "[monitoring]\n")
	// The hub's address cannot resolve: said once, as an error.
	write(t, root, "forts/hub.toml", "[cluster]\n")
	if s, err := gate(t); err == nil || !contains(s, "no mesh_ip") {
		t.Errorf("no mesh_ip:\n%s", s)
	}
	// One fort, not --all: its own findings.
	write(t, root, "forts/hub.toml", "mesh_ip = \"10.9.0.2\"\n[cluster]\n")
	var out bytes.Buffer
	if err := check([]string{"plain"}, &out); err != nil || !contains(out.String(), "== plain", plainNote) || contains(out.String(), "== layers/monitoring") {
		t.Errorf("one fort: %v\n%s", err, out.String())
	}
	// No hub in the core: one ok line, and the strays still count.
	write(t, root, "estate.toml", "name = \"test\"\n[source]\nrepo = \""+testRepo+"\"\n")
	write(t, root, role.FortPath("edge"), hubFile)
	if s, err := gate(t); err == nil || !contains(s, "hub   none", "[monitoring.hub] on a fort estate.toml does not name") {
		t.Errorf("no hub: %v\n%s", err, s)
	}
}

// A hub in another estate: HUB_MESH_IP is the remotes entry, the hub's
// files say so, and a missing entry is refused by name.
func TestRemoteHub(t *testing.T) {
	root := estate(t)
	write(t, root, "estate.toml", "name = \"staging\"\n[source]\nrepo = \""+testRepo+"\"\n[hubs.monitoring]\nestate = \""+remoteRepo+"\"\nfort = \"central\"\n")
	if _, _, err := planScaffold(root, "edge"); err == nil || !contains(err.Error(), "remotes") {
		t.Errorf("no peers: %v", err)
	}
	write(t, root, "mesh/peers.json", `{"remotes": {"central": {"wg_vpn_ip": "10.9.0.6"}}}`)
	p, _, err := planScaffold(root, "edge")
	if err != nil {
		t.Fatal(err)
	}
	apply(t, p)
	if v := read(t, root, clusterVarsPath("edge")); !contains(v, "HUB_MESH_IP: 10.9.0.6") {
		t.Errorf("cluster-vars:\n%s", v)
	}
	if !kit.Exists(root, hubFilePath("central", "edge")) || contains(read(t, root, hubKustomization("central")), "except through") {
		t.Error("the remote hub's directory")
	}
	var out bytes.Buffer
	check([]string{"--all"}, &out)
	if !contains(out.String(), "central at 10.9.0.6 (in oss/estate-hub, through the link)") {
		t.Errorf("check:\n%s", out.String())
	}
}

func TestDismantleRemovesTheFortsFiles(t *testing.T) {
	root := estate(t)
	scaffoldAll(t, root)
	p, notes, err := planDismantle(root, "edge")
	if err != nil {
		t.Fatal(err)
	}
	apply(t, p)
	if kit.Exists(root, hubFilePath("hub", "edge")) || kit.Exists(root, role.FortPath("edge")) || contains(read(t, root, hubKustomization("hub")), "edge.yaml") {
		t.Error("edge's files remain")
	}
	if !kit.Exists(root, hubFilePath("hub", "hub")) || !kit.Exists(root, filepath.Join(monitoringConfigs, pushFile)) || !kit.Exists(root, stackDir("hub")) || !kit.Exists(root, filepath.Join(monitoringConfigs, "prometheus-edge.yaml")) {
		t.Error("too much removed")
	}
	if len(notes) != 1 || !contains(notes[0], clusterVarsPath("edge")) {
		t.Errorf("notes: %v", notes)
	}
	var out bytes.Buffer
	if err := check([]string{"--all"}, &out); err == nil || !contains(out.String(), "✗ "+role.FortPath("edge")) {
		t.Errorf("after dismantle the core still declares edge: %v\n%s", err, out.String())
	}
	// The hub itself: the warnings that everything points at it.
	_, notes, err = planDismantle(root, "hub")
	if err != nil || len(notes) != 2 || !contains(notes[0], "this estate's monitoring hub") || !contains(notes[1], clusterVarsPath("hub")) {
		t.Errorf("hub: %v %v", err, notes)
	}
	// A fort with nothing rendered: every path present, nothing planned.
	write(t, root, "forts/fresh.toml", "mesh_ip = \"10.9.0.7\"\n")
	p, _, err = planDismantle(root, "fresh")
	if err != nil || len(p.Actions) != 0 {
		t.Errorf("fresh: %v %+v", err, p.Actions)
	}
}

// Retiring the estate's last fort empties the hub's directory: the
// kustomization keeps `resources: []` — kustomize refuses a null list —
// a note says the hub applies nothing until a fort is scaffolded, the
// gate stays green on the empty estate, and the next scaffold reopens
// the list (fort-layers#26 in the internal set is the precedent).
func TestDismantleLastFortLeavesAnEmptyList(t *testing.T) {
	root := estate(t)
	write(t, root, "estate.toml", "name = \"staging\"\n[source]\nrepo = \""+testRepo+"\"\n[hubs.monitoring]\nestate = \""+remoteRepo+"\"\nfort = \"central\"\n")
	write(t, root, "mesh/peers.json", `{"remotes": {"central": {"wg_vpn_ip": "10.9.0.6"}}}`)
	p, _, err := planScaffold(root, "edge")
	if err != nil {
		t.Fatal(err)
	}
	apply(t, p)
	p, notes, err := planDismantle(root, "edge")
	if err != nil {
		t.Fatal(err)
	}
	apply(t, p)
	k := read(t, root, hubKustomization("central"))
	if contains(k, "edge.yaml") || !strings.HasSuffix(k, "\nresources: []\n") {
		t.Errorf("kustomization:\n%s", k)
	}
	emptied := false
	for _, n := range notes {
		emptied = emptied || contains(n, "the hub applies nothing for this estate until a fort is scaffolded")
	}
	if !emptied {
		t.Errorf("notes: %v", notes)
	}
	// The retired estate: the core's dismantle removed the manifests.
	// The gate must stay green — a red gate on an empty estate is noise.
	for _, f := range []string{"forts/edge.toml", "forts/hub.toml", "forts/plain.toml"} {
		os.Remove(filepath.Join(root, f))
	}
	if s, err := gate(t); err != nil || strings.Contains(s, "✗") {
		t.Errorf("the empty estate's gate: %v\n%s", err, s)
	}
	// The next fort reopens the list.
	write(t, root, "forts/edge.toml", "mesh_ip = \"10.9.0.3\"\n[cluster]\n")
	p, _, err = planScaffold(root, "edge")
	if err != nil {
		t.Fatal(err)
	}
	apply(t, p)
	if k := read(t, root, hubKustomization("central")); !contains(k, "resources:\n  - edge.yaml") || contains(k, "[]") {
		t.Errorf("reopened kustomization:\n%s", k)
	}
}

// A second layer swaps the apps entry from the stack to its own overlay
// directory, which includes the stack through kustomize and patches it
// (#4). check accepts that shape, one hop deep, and still errors when no
// include path reaches the stack.
func TestCheckAcceptsAnOverlayDirectory(t *testing.T) {
	root := estate(t)
	scaffoldAll(t, root)
	apps := filepath.Join(appsDir("hub"), "kustomization.yaml")
	base := read(t, root, apps)
	write(t, root, apps, strings.Replace(base, "- "+stackName, "- monitoring-estates", 1))
	overlay := filepath.Join(appsDir("hub"), "monitoring-estates", "kustomization.yaml")
	write(t, root, overlay, "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n  - ../"+stackName+"\npatches:\n  - path: thanos-sidecar.yaml\n")
	if s, err := gate(t); err != nil {
		t.Fatalf("overlay: %v\n%s", err, s)
	}
	// The overlay includes something else: no path reaches the stack.
	write(t, root, overlay, "resources:\n  - ../elsewhere\n")
	if s, err := gate(t); err == nil || !contains(s, "does not reach "+stackName) {
		t.Errorf("unreached stack:\n%s", s)
	}
}
