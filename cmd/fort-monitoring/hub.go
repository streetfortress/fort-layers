package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"forge.streetfortress.cloud/oss/fort-layers/internal/kit"
)

// The hub's side of `fort monitoring scaffold <fort>`: what the fort that
// serves the role renders, from its [monitoring.hub] table. Everything
// is appends and new files, never a rewrite:
//
//	flux/apps/<fort>/monitoring-central/   the central stack: the namespace, the central
//	                                  Prometheus (the receiver, the history, the fleet
//	                                  rules), the central Alertmanager and its committed
//	                                  config, the two mesh NodePorts, Grafana and its
//	                                  route, Loki on the filesystem, the central
//	                                  ServiceMonitors, the contract; grafana-admin
//	                                  listed and absent until mint
//	flux/apps/<fort>/serves/          the subscription to this estate's flux/hub/<fort>/
//	flux/apps/<fort>/kustomization.yaml   + monitoring-central, + serves
//	flux/apps/<fort>/gateway/         a Certificate and a listener patch for
//	                                  grafana.<hostname>
//	nixos/hosts/<fort>/default.nix    the module that opens 30990 and 31100 on the
//	                                  mesh interface
//
// The gateway is a battery another role (or a hand) laid down: the role
// names its location and appends to it. A directory that is not there is
// an error that names what renders it.

// stackView is what the stack templates render from.
type stackView struct {
	Fort, Estate, Hostname, FortFile string
	Metrics, Logs                    string
}

// serveView is the subscription the hub fort's tree carries: its own
// estate's. A layer built on this one adds subscriptions for estates
// served across repos.
type serveView struct {
	Hub, Name string
}

// The stack's files, in the kustomization's order. A .gotpl is rendered
// with the view; a plain file is copied.
var stackFiles = []struct{ name, note string }{
	{"namespace.yaml", "monitoring-central, apart from the edge stack's namespace"},
	{"central-prometheus.yaml", "the remote-write receiver and the fleet rules; the history, on the fort's disk"},
	{"servicemonitors-central.yaml", "the operator-managed half, scraped by the hub's edge"},
	{"alertmanager-central.yaml", "the central Alertmanager"},
	{"alertmanager-config.yaml", "the committed config: a null receiver until the estate opts a notifier in"},
	{"mesh-services.yaml", "NodePorts 30990 (remote_write) and 31100 (Loki push)"},
	{"grafana.yaml", "grafana.<hostname>; the edge Alertmanagers arrive as datasources"},
	{"loki.yaml", "single binary, chunks on the filesystem, the OTLP label set"},
	{"loki-rules.yaml", "the estate's log rules: NixosUpgradeFailed"},
	{"blackbox.yaml", "the prober; probes are the estate's ScrapeConfigs"},
	{"dashboards/estate-overview.json", "Grafana dashboard: the estate's revisions, alerts, mesh and ingest"},
	{"dashboards/fort-overview.json", "Grafana dashboard: one fort's host, its units and its logs"},
	{"forts.inventory.yaml", "grafana-admin's seeding record"},
	{"kustomization.yaml", "the list; grafana-admin.sops.yaml listed-but-absent until mint"},
}

// stackFileNames is what check requires: the stack's files.
func stackFileNames() []string {
	var out []string
	for _, f := range stackFiles {
		out = append(out, f.name)
	}
	return out
}

// stackSecrets are the files mint writes into the stack directory.
var stackSecrets = []string{"grafana-admin.sops.yaml"}

// The name the stack serves on the fort's Gateway: the listener's name
// and the host's first label.
var gatewayNames = []struct{ name, label string }{{"grafana", "grafana"}}

func hubModuleRef(input string) string { return "inputs." + input + ".nixosModules.monitoring-hub" }

const hubNixBlock = `  # This fort is a monitoring hub (fort-layers, layers/monitoring/
  # forts/<fort>.toml [monitoring.hub]): the module opens the central
  # Prometheus's remote_write (30990) and Loki's push (31100) on the mesh
  # interface for this estate's forts.
  sfi.monitoring-hub.enable = true;
`

// serves is the subscription the fort's tree carries: its own estate's
// forts, through the flux-system source.
func serves(core coreEstate, fort string) []serveView {
	return []serveView{{Hub: fort, Name: shortName(core.Source.Repo)}}
}

// planHub adds the hub's renders to the plan.
func planHub(root, fort string, core coreEstate, hub *Hub, p *kit.Plan) error {
	apps := filepath.Join(appsDir(fort), "kustomization.yaml")
	if !kit.Exists(root, apps) {
		return fmt.Errorf("no %s — fort scaffold %s renders the apps tree this layer adds the stack to", apps, fort)
	}
	gw := gatewayDir(fort)
	if !kit.Exists(root, filepath.Join(gw, "kustomization.yaml")) {
		return fmt.Errorf("no %s/kustomization.yaml — the stack's route attaches to the fort's Gateway (a gateway battery renders it; lay one down first)", gw)
	}
	hostNix := hostNixPath(fort)
	if !kit.Exists(root, hostNix) {
		return fmt.Errorf("no %s — fort scaffold %s renders the host file this layer opens the ports in", hostNix, fort)
	}

	// The stack.
	view := stackView{
		Fort: fort, Estate: core.Name, Hostname: hub.Hostname, FortFile: role.FortPath(fort),
		Metrics: hub.RetMetrics(), Logs: hub.RetLogs(),
	}
	for _, f := range stackFiles {
		var content []byte
		var err error
		if content, err = templates.ReadFile("templates/stack/" + f.name + ".gotpl"); err == nil {
			content, err = render("stack/"+f.name+".gotpl", view)
		} else {
			content, err = templates.ReadFile("templates/stack/" + f.name)
		}
		if err != nil {
			return err
		}
		p.New(filepath.Join(stackDir(fort), f.name), f.note, content)
	}

	// The subscription.
	sv := serves(core, fort)
	for _, s := range sv {
		content, err := render("serve.yaml.gotpl", s)
		if err != nil {
			return err
		}
		p.New(servesPath(fort, s.Name), "this estate's own forts: "+hubDir(fort)+"/ through the flux-system source", content)
	}
	skust := filepath.Join(servesDir(fort), "kustomization.yaml")
	if kit.Exists(root, skust) {
		if err := p.Append(skust, "+ the subscription", func(s string) (string, error) {
			for _, v := range sv {
				if kit.ListsResource(s, v.Name+".yaml") {
					continue
				}
				var err error
				if s, err = kit.AppendListItem(s, "resources", v.Name+".yaml"); err != nil {
					return "", err
				}
			}
			return s, nil
		}); err != nil {
			return err
		}
	} else {
		content, err := render("serves-kustomization.yaml.gotpl", struct {
			Fort   string
			Serves []serveView
		}{fort, sv})
		if err != nil {
			return err
		}
		p.New(skust, "the hub's subscriptions", content)
	}
	if err := p.Append(apps, "+ "+stackName+", "+servesName, func(s string) (string, error) {
		for _, item := range []string{stackName, servesName} {
			if kit.ListsResource(s, item) {
				continue
			}
			var err error
			if s, err = kit.AppendListItem(s, "resources", item); err != nil {
				return "", err
			}
		}
		return s, nil
	}); err != nil {
		return err
	}

	// The gateway: a certificate and a listener.
	issuer := detectIssuer(root, gw)
	gkust := filepath.Join(gw, "kustomization.yaml")
	for _, g := range gatewayNames {
		host := g.label + "." + hub.Hostname
		v := struct{ Fort, Name, Host, Issuer string }{fort, g.name, host, issuer}
		cert, err := render("certificate.yaml.gotpl", v)
		if err != nil {
			return err
		}
		listener, err := render("listener.yaml.gotpl", v)
		if err != nil {
			return err
		}
		p.New(filepath.Join(gw, "certificate-"+g.name+".yaml"), host+", issued by "+issuer, cert)
		p.New(filepath.Join(gw, "listener-"+g.name+".yaml"), "https-"+g.name+" on the Gateway", listener)
	}
	if err := p.Append(gkust, "+ the certificate, + the listener patch", func(s string) (string, error) {
		for _, g := range gatewayNames {
			cert := "certificate-" + g.name + ".yaml"
			if !kit.ListsResource(s, cert) {
				var err error
				if s, err = kit.AppendListItem(s, "resources", cert); err != nil {
					return "", err
				}
			}
			listener := "listener-" + g.name + ".yaml"
			if !strings.Contains(s, "path: "+listener) {
				s = kit.AppendPatch(s, "# One JSON6902 patch per service appends its https listener to the\n# Gateway: listeners live in a single object, and a strategic-merge\n# patch cannot append to a CRD list.\n", listener, "gateway.networking.k8s.io", "v1", "Gateway", "homelab", "traefik")
			}
		}
		return s, nil
	}); err != nil {
		return err
	}

	// The host: the ports.
	importLine := hubModuleRef(role.Input(root)) + "  # the hub's ingest ports on the mesh (fort-layers, " + role.FortPath(fort) + ")"
	return p.Append(hostNix, "+ the monitoring-hub module: 30990, 31100 on the mesh interface", func(s string) (string, error) {
		s, err := kit.NixEnsureImport(s, importLine)
		if err != nil {
			return "", err
		}
		return kit.NixEnsureBlock(s, "sfi.monitoring-hub", hubNixBlock)
	})
}

var issuerRE = regexp.MustCompile(`(?m)^\s+issuerRef:\s*\n(?:\s+kind:.*\n)?\s+name:\s*(\S+)`)

// detectIssuer reads the ClusterIssuer the Gateway's other certificates
// name, so the stack's certificate comes from the same CA as the rest
// of the fort; the core's letsencrypt where there is none.
func detectIssuer(root, gw string) string {
	matches, _ := filepath.Glob(filepath.Join(root, gw, "certificate-*.yaml"))
	for _, m := range matches {
		raw, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		if sub := issuerRE.FindSubmatch(raw); sub != nil {
			return string(sub[1])
		}
	}
	return "letsencrypt"
}
