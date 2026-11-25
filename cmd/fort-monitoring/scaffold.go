package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"forge.streetfortress.cloud/oss/fort-layers/internal/kit"
)

// `fort monitoring scaffold <fort>` renders the fort's side of the
// layer, as appends and new files — never a rewrite — and prints the
// marked tree. What it renders:
//
//	flux/infrastructure/configs/monitoring/    a cluster fort, once per estate: the edge
//	                                      stack the core retired (forts#270) — the edge
//	                                      Prometheus and Alertmanager, the committed
//	                                      config, node-exporter's scrape seam, the rules,
//	                                      the mesh NodePorts, the ops read rule; listed in
//	                                      the configs kustomization
//	flux/infrastructure/controllers/…     the three controllers the stack needs:
//	                                      prometheus-operator, its CRDs, kube-state-metrics
//
// and, when estate.toml names a [hubs.monitoring]:
//
//	layers/monitoring/forts/<fort>.toml   [monitoring], when the file is not there; with the
//	                                      [monitoring.hub] shape on the fort that serves the role
//	flux/hub/<hub>/<fort>.yaml            a cluster fort: the dead man's switch, the Loki
//	                                      rule, the edge Alertmanager as a Grafana
//	                                      datasource; a plain fort: the hub's scrape
//	                                      target over the mesh and HostDown
//	flux/hub/<hub>/kustomization.yaml     + <fort>.yaml (the file, on the estate's first fort)
//	flux/clusters/<fort>/cluster-vars.yaml   + HUB_MESH_IP, the hub's address on this mesh
//	flux/infrastructure/configs/monitoring/push-metrics.yaml   once per estate ([push] metrics):
//	                                      the remoteWrite as a kustomize patch on the edge
//	                                      Prometheus, listed in the directory's patches
//	flux/infrastructure/controllers/fluent-bit/   once per estate ([push] logs): the log
//	                                      shipper, listed in the controllers kustomization
//
// and on the fort that serves the role, from its [monitoring.hub] table,
// the hub's renders (hub.go). A hub fort with no file yet gets the file
// with the hub's shape and stops there: the stack renders from the
// refined table on the next run.
//
// Everything about ONE fort lives in its own file (forts#288): the hub
// has to know each cluster by name — absent(up{cluster="<fort>"}) is the
// only expression that fires when a cluster goes silent — and the file
// sits in the fort's OWN estate, under the hub fort's name, so a hub
// in another estate reads it through a flux subscription and nothing is
// written across repos.

// hubView is what the hub-file templates render from.
type hubView struct {
	Fort, Hub, Estate, MeshIP, FortFile string
	// IsHub: this fort is the hub, so its edge Alertmanager is reached
	// through cluster DNS, not over the mesh.
	IsHub bool
	// HubLocal: the hub is a fort of this estate.
	HubLocal bool
}

func scaffoldCmd(args []string) error {
	fs := flag.NewFlagSet("scaffold", flag.ContinueOnError)
	var pos []string
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		pos = append(pos, args[0])
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(pos) != 1 || fs.NArg() > 0 {
		return errors.New("usage: fort monitoring scaffold <fort>")
	}
	fort := pos[0]
	root, err := kit.Root()
	if err != nil {
		return err
	}
	p, summary, err := planScaffold(root, fort)
	if err != nil {
		return err
	}
	if err := p.Apply(); err != nil {
		return err
	}
	if err := p.Stage(); err != nil {
		return err
	}
	p.Report(os.Stdout, "fort monitoring scaffold "+fort)
	fmt.Fprintln(os.Stdout, "\n"+summary)
	reportNext(root, fort)
	return nil
}

// planScaffold builds the plan for one fort. Returns the shape's note
// for the report's title.
func planScaffold(root, fort string) (*kit.Plan, string, error) {
	if !kit.ValidFortName(fort) {
		return nil, "", fmt.Errorf("%q is not a fort name", fort)
	}
	if !kit.Exists(root, filepath.Join(kit.CoreFortsDir, fort+".toml")) {
		return nil, "", fmt.Errorf("no %s/%s.toml — the core declares no such fort (fort scaffold renders a fort's files from its manifest first)", kit.CoreFortsDir, fort)
	}
	est, err := loadEstate(root)
	if err != nil {
		return nil, "", err
	}
	core, err := loadCore(root)
	if err != nil {
		return nil, "", err
	}
	cf, err := loadCoreFort(root, fort)
	if err != nil {
		return nil, "", err
	}
	if cf.MeshIP == "" {
		return nil, "", fmt.Errorf("%s/%s.toml: no mesh_ip — every fort is a mesh member, and the hub reaches a plain fort at it", kit.CoreFortsDir, fort)
	}
	h, hasHub := core.hub()
	var hubIP string
	if hasHub {
		if hubIP, err = hubMeshIP(root, core, h); err != nil {
			return nil, "", err
		}
	}
	p := &kit.Plan{Root: root}
	isHub := hasHub && core.isHubFort(fort)

	// The fort's own file: the positive declaration that the hub watches
	// this fort — written when it is not there, validated when it is. A
	// hub fort's file carries the hub's table, and the stack renders from
	// the refined file. With no hub in the core there is no file to
	// write: the edge stack is the whole render.
	var hub *Hub
	if hasHub {
		if kit.Exists(root, role.FortPath(fort)) {
			f, err := loadFort(root, fort)
			if err != nil {
				return nil, "", err
			}
			hub = f.Monitoring.Hub
			switch {
			case hub != nil && !isHub:
				return nil, "", fmt.Errorf("%s has [monitoring.hub], and estate.toml does not name %s its [hubs.monitoring] fort — drop the table, or declare the hub in the core (forts' docs/hub.md)", role.FortPath(fort), fort)
			case hub == nil && isHub:
				return nil, "", fmt.Errorf("%s serves the role (estate.toml) and %s has no [monitoring.hub] — the stack renders from it; add the table (docs/monitoring.md; a fresh file from fort monitoring scaffold has the shape)", fort, role.FortPath(fort))
			}
		} else {
			if !cf.IsCluster() && isHub {
				return nil, "", fmt.Errorf("%s serves the role and is a plain fort (no [cluster]) — a hub runs the stack on flux; the core's manifest decides", fort)
			}
			content, err := render("fort.toml.gotpl", struct {
				Fort, Hub, Hostname, Why string
				IsHub                    bool
			}{fort, h.Fort, fort + "." + core.zone(), "this estate's [hubs.monitoring]", isHub})
			if err != nil {
				return nil, "", err
			}
			note := "[monitoring]: the hub " + h.Fort + " watches this fort"
			if isHub {
				note = "[monitoring] and the [monitoring.hub] shape — refine it (hostname, retention), then run again for the stack"
			}
			p.New(role.FortPath(fort), note, content)
		}
	}

	// The edge stack, once per estate: every cluster fort applies it.
	if cf.IsCluster() {
		if err := planEdge(root, est, hasHub, p); err != nil {
			return nil, "", err
		}
	}

	note := edgeNote
	if hasHub {
		view := hubView{
			Fort: fort, Hub: h.Fort, Estate: core.Name, MeshIP: cf.MeshIP, FortFile: role.FortPath(fort),
			HubLocal: core.hubLocal(h),
		}
		view.IsHub = view.HubLocal && fort == h.Fort
		tmpl := "host-monitoring.yaml.gotpl"
		note = plainNote
		if cf.IsCluster() {
			tmpl, note = "cluster-monitoring.yaml.gotpl", clusterNote
		}
		file, err := render(tmpl, view)
		if err != nil {
			return nil, "", err
		}
		p.New(hubFilePath(h.Fort, fort), note, file)
		item := fort + ".yaml"
		if kit.Exists(root, hubKustomization(h.Fort)) {
			if err := p.Append(hubKustomization(h.Fort), "+ "+item, func(s string) (string, error) {
				if kit.ListsResource(s, item) {
					return s, nil
				}
				return kit.AppendListItem(s, "resources", item)
			}); err != nil {
				return nil, "", err
			}
		} else {
			kust, err := render("hub-kustomization.yaml.gotpl", view)
			if err != nil {
				return nil, "", err
			}
			p.New(hubKustomization(h.Fort), "the directory the hub subscribes to: "+item, kust)
		}

		if cf.IsCluster() {
			// The hub's address, for the push the shared layer carries.
			vars := clusterVarsPath(fort)
			if !kit.Exists(root, vars) {
				return nil, "", fmt.Errorf("no %s — fort scaffold %s renders the cluster's files this layer appends to", vars, fort)
			}
			if err := p.Append(vars, "+ "+hubVar+" "+hubIP+" — the hub's address on this mesh", func(s string) (string, error) {
				return kit.EnsureClusterVar(s, hubVar, hubIP)
			}); err != nil {
				return nil, "", err
			}
			// The push, once per estate.
			if est.PushMetrics() {
				if err := planPush(root, p); err != nil {
					return nil, "", err
				}
			}
			if est.PushLogs() {
				kust := filepath.Join(controllersDir, "kustomization.yaml")
				if !kit.Exists(root, kust) {
					return nil, "", fmt.Errorf("no %s — the core's fort init renders the controllers this layer adds the log shipper to", kust)
				}
				for _, f := range []string{"kustomization.yaml", "repository.yaml", "release.yaml"} {
					content, err := templates.ReadFile("templates/fluent-bit/" + f)
					if err != nil {
						return nil, "", err
					}
					notes := map[string]string{"release.yaml": "fluent-bit ships container, journal and audit logs to http://${" + hubVar + "}:31100", "repository.yaml": "the fluent helm repository", "kustomization.yaml": "the controller's list"}
					p.New(filepath.Join(fluentBitDir, f), notes[f], content)
				}
				if err := p.Append(kust, "+ "+fluentBitName, func(s string) (string, error) {
					if kit.ListsResource(s, fluentBitName) {
						return s, nil
					}
					return kit.AppendListItem(s, "resources", fluentBitName)
				}); err != nil {
					return nil, "", err
				}
			}
		}
	}

	// The hub's side, from the refined table.
	if hub != nil {
		if err := planHub(root, fort, core, hub, p); err != nil {
			return nil, "", err
		}
		note = hubNote
	}
	return p, note, nil
}

// planEdge plans the edge stack into the shared infrastructure: the
// files, their kustomization, the configs listing, and the three
// controllers. Idempotent file by file — a re-run fills gaps, an
// existing file is the estate's and is never rewritten.
func planEdge(root string, est Estate, hasHub bool, p *kit.Plan) error {
	if !kit.Exists(root, configsKust) {
		return fmt.Errorf("no %s — the core's fort init renders the shared configs this layer adds the edge stack to", configsKust)
	}
	ctrlKust := filepath.Join(controllersDir, "kustomization.yaml")
	if !kit.Exists(root, ctrlKust) {
		return fmt.Errorf("no %s — the core's fort init renders the controllers this layer adds the operator to", ctrlKust)
	}
	for _, f := range edgeFiles {
		content, err := templates.ReadFile("templates/edge/" + f.name)
		if err != nil {
			return err
		}
		p.New(filepath.Join(monitoringConfigs, f.name), f.note, content)
	}
	kust := filepath.Join(monitoringConfigs, "kustomization.yaml")
	if !kit.Exists(root, kust) {
		content, err := render("edge/kustomization.yaml.gotpl", struct{ Push bool }{hasHub && est.PushMetrics()})
		if err != nil {
			return err
		}
		p.New(kust, "the list", content)
	}
	if err := p.Append(configsKust, "+ monitoring", func(s string) (string, error) {
		if kit.ListsResource(s, "monitoring") {
			return s, nil
		}
		return kit.AppendListItem(s, "resources", "monitoring")
	}); err != nil {
		return err
	}
	fileNotes := map[string]string{
		"kustomization.yaml": "the list",
		"namespace.yaml":     "the namespace",
		"repository.yaml":    "the prometheus-community helm repository",
	}
	for _, c := range edgeControllers {
		for _, f := range c.files {
			content, err := templates.ReadFile("templates/controllers/" + c.name + "/" + f)
			if err != nil {
				return err
			}
			note, ok := fileNotes[f]
			if !ok {
				note = c.note
			}
			p.New(filepath.Join(controllersDir, c.name, f), note, content)
		}
		name := c.name
		if err := p.Append(ctrlKust, "+ "+name, func(s string) (string, error) {
			if kit.ListsResource(s, name) {
				return s, nil
			}
			return kit.AppendListItem(s, "resources", name)
		}); err != nil {
			return err
		}
	}
	return nil
}

// planPush plans the remote-write patch beside the edge Prometheus. The
// patch entry rides in the edge kustomization: included at its render
// when the estate already pushes, appended when the hub (or the push)
// arrives later.
func planPush(root string, p *kit.Plan) error {
	patch, err := templates.ReadFile("templates/" + pushFile)
	if err != nil {
		return err
	}
	p.New(filepath.Join(monitoringConfigs, pushFile), "the edge Prometheus remote-writes to http://${"+hubVar+"}:30990", patch)
	kust := filepath.Join(monitoringConfigs, "kustomization.yaml")
	if !kit.Exists(root, kust) {
		return nil // rendered this run, with the patch entry included
	}
	return p.Append(kust, "+ patches: "+pushFile, func(s string) (string, error) {
		if strings.Contains(s, "path: "+pushFile) {
			return s, nil
		}
		return kit.AppendPatch(s, "# The push (layers/monitoring/estate.toml [push] metrics): a patch on the\n# edge Prometheus, so the rendered file above is never edited.\n", pushFile, "monitoring.coreos.com", "v1", "Prometheus", "edge", "monitoring"), nil
	})
}

// reportNext prints what happens after a written scaffold: who applies
// the file, and the hub's next step.
func reportNext(root, fort string) {
	core, err := loadCore(root)
	if err != nil {
		return
	}
	h, ok := core.hub()
	if !ok {
		fmt.Fprintln(os.Stdout, "\nNext: Commit and Open a PR.")
		return
	}
	if core.isHubFort(fort) {
		if hubOf(root, fort) == nil {
			fmt.Fprintf(os.Stdout, "\nNext: refine %s (hostname, retention) and run fort monitoring scaffold %s again.\n", role.FortPath(fort), fort)
			return
		}
		fmt.Fprintf(os.Stdout, "\nNext: fort monitoring mint %s (the stack's credential), and open a PR.\n", fort)
		return
	}
	if core.hubLocal(h) {
		fmt.Fprintf(os.Stdout, "\nNext: Commit and Open a PR. %s applies %s through its subscription to %s/.\n", h.Fort, hubFilePath(h.Fort, fort), hubDir(h.Fort))
	} else {
		fmt.Fprintf(os.Stdout, "\nNext: Commit and Open a PR. %s (%s) applies %s through its subscription to this repo's %s/; that estate serves this one the role.\n", h.Fort, ref(h.Estate), hubFilePath(h.Fort, fort), hubDir(h.Fort))
	}
}
