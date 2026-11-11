package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"forge.streetfortress.cloud/oss/fort-layers/internal/kit"
	"forge.streetfortress.cloud/oss/fort-layers/internal/tools"
)

// `fort monitoring check` is the layer's gate (forts' docs/layers.md:
// the estate's gate runs it after the core's). It reads the tree alone
// and says whether what the files declare is what the tree carries:
//
//	the estate file    parses; the push it declares is rendered and listed
//	the edge stack     rendered and listed once any cluster fort exists; the
//	                   three controllers listed; the core's ops-readonly.yaml
//	                   aggregates the layer's read rule
//	the hub            when the core names one, its address on this mesh resolves
//	every core fort    has its [monitoring] file, and its file in flux/hub/<hub>/,
//	                   listed — a fort with no dead man's switch is the silent
//	                   failure this layer exists to catch
//	flux/hub/<hub>/    builds, and holds no file for a fort the core does not
//	                   declare — a retired fort's rules would page for a cluster
//	                   that is gone
//	each cluster       HUB_MESH_IP in its cluster-vars is the hub's address
//	the hub fort       the core's declaration and the fort's [monitoring.hub] agree;
//	                   the stack is there, builds, and the apps kustomization
//	                   reaches it — directly, or through a second layer's overlay
//	                   entry whose kustomization includes it (#4); the subscription
//	                   is there and listed; the gateway has the name; the host
//	                   opens the ports; the credential exists (mint)
func check(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	all := fs.Bool("all", false, "every fort of the estate, and the estate findings")
	var pos []string
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		pos = append(pos, args[0])
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if (*all && len(pos) > 0) || (!*all && len(pos) != 1) || fs.NArg() > 0 {
		return errors.New("usage: fort monitoring check --all | fort monitoring check <fort>")
	}
	root, err := kit.Root()
	if err != nil {
		return err
	}
	annotate := kit.Annotate()
	errs := 0

	core, err := kit.CoreForts(root)
	if err != nil {
		return err
	}
	if !role.Installed(root) {
		errs += kit.Report(w, role.Dir(), []kit.Finding{{Level: kit.Error, Dir: role.EstatePath(), Msg: "missing — fort monitoring init --write writes it (fort layer add runs it)"}}, annotate)
		return fmt.Errorf("%d error(s)", errs)
	}
	est, err := loadEstate(root)
	if err != nil {
		errs += kit.Report(w, role.Dir(), []kit.Finding{{Level: kit.Error, Dir: role.EstatePath(), Msg: strings.TrimPrefix(err.Error(), filepath.Join(root, role.EstatePath())+": ")}}, annotate)
		return fmt.Errorf("%d error(s)", errs)
	}
	ce, err := loadCore(root)
	if err != nil {
		errs += kit.Report(w, role.Dir(), []kit.Finding{{Level: kit.Error, Dir: "estate.toml", Msg: err.Error()}}, annotate)
		return fmt.Errorf("%d error(s)", errs)
	}
	ctx := checkCtx{root: root, est: est, core: ce, forts: core}
	if h, ok := ce.hub(); ok {
		ctx.hub, ctx.hasHub = h, true
		ctx.hubIP, ctx.hubErr = hubMeshIP(root, ce, h)
	}

	var forts []string
	if *all {
		forts = core
	} else {
		forts = pos
	}
	if *all {
		errs += kit.Report(w, role.Dir(), ctx.estateFindings(), annotate)
	}
	for _, f := range forts {
		errs += kit.Report(w, f, ctx.fortFindings(f), annotate)
	}
	if errs > 0 {
		return fmt.Errorf("%d error(s)", errs)
	}
	return nil
}

type checkCtx struct {
	root   string
	est    Estate
	core   coreEstate
	forts  []string
	hub    coreHub
	hasHub bool
	hubIP  string
	hubErr error
}

// estateFindings is the estate half: the edge stack, the hub, the push,
// the strays and the orphans, the build of the hub's directory, and the
// hub fort's declarations at both ends.
func (c checkCtx) estateFindings() []kit.Finding {
	out := []kit.Finding{{Level: kit.OK, Dir: role.EstatePath(), Msg: c.est.Summary()}}
	declared, err := role.FortNames(c.root)
	if err != nil {
		return append(out, kit.Finding{Level: kit.Error, Dir: role.FortsDir(), Msg: err.Error()})
	}
	for _, f := range declared {
		if !slices.Contains(c.forts, f) {
			out = append(out, kit.Finding{Level: kit.Error, Dir: role.FortPath(f), Msg: "no " + kit.CoreFortsDir + "/" + f + ".toml in the core — a stray, or a fort the core dismantled: fort monitoring dismantle " + f + " first"})
		}
	}

	// The edge stack, once any cluster fort exists.
	anyCluster := false
	for _, f := range c.forts {
		if cf, err := loadCoreFort(c.root, f); err == nil && cf.IsCluster() {
			anyCluster = true
			break
		}
	}
	scaffoldAny := "fort monitoring scaffold <a cluster fort>"
	if anyCluster {
		var missing []string
		for _, f := range append(edgeFileNames(), "kustomization.yaml") {
			if !kit.Exists(c.root, filepath.Join(monitoringConfigs, f)) {
				missing = append(missing, f)
			}
		}
		switch {
		case len(missing) > 0:
			out = append(out, kit.Finding{Level: kit.Error, Dir: monitoringConfigs, Msg: "lacks " + strings.Join(missing, ", ") + " — the core renders no observability (forts#270); " + scaffoldAny + " renders the edge stack"})
		case !kit.KustomizationLists(c.root, filepath.Dir(configsKust), "monitoring"):
			out = append(out, kit.Finding{Level: kit.Error, Dir: configsKust, Msg: "does not list monitoring — the edge stack is there and flux never applies it; " + scaffoldAny + " appends it"})
		default:
			out = append(out, kit.Finding{Level: kit.OK, Dir: monitoringConfigs, Msg: "the edge stack: Prometheus (24h local), Alertmanager (committed config), node-exporter, the rules, the ops read rule"})
		}
		for _, ctrl := range edgeControllers {
			dir := filepath.Join(controllersDir, ctrl.name)
			switch {
			case !kit.Exists(c.root, filepath.Join(dir, "release.yaml")):
				out = append(out, kit.Finding{Level: kit.Error, Dir: dir, Msg: "missing — the edge stack needs it; " + scaffoldAny + " renders it"})
			case !kit.KustomizationLists(c.root, controllersDir, ctrl.name):
				out = append(out, kit.Finding{Level: kit.Error, Dir: filepath.Join(controllersDir, "kustomization.yaml"), Msg: "does not list " + ctrl.name + " — the controller is there and flux never applies it; " + scaffoldAny + " appends it"})
			}
		}
		if !kit.OpsAggregates(c.root) {
			out = append(out, kit.Finding{Level: kit.Error, Dir: kit.OpsReadFile, Msg: kit.OpsMissingMsg})
		}
	}

	out = append(out, c.hubDeclarationFindings(declared)...)
	if !c.hasHub {
		return append(out, kit.Finding{Level: kit.OK, Dir: "estate.toml", Msg: "no [hubs.monitoring]: no fort's file for a hub, no push, and no dead man's switch for any of them — the edge Alertmanager is the only alerting"})
	}
	if c.hubErr != nil {
		return append(out, kit.Finding{Level: kit.Error, Dir: "estate.toml", Msg: c.hubErr.Error()})
	}
	where := "a fort of this estate"
	if !c.core.hubLocal(c.hub) {
		where = "in " + ref(c.hub.Estate) + ", through the link"
	}
	out = append(out, kit.Finding{Level: kit.OK, Dir: "estate.toml", Msg: fmt.Sprintf("[hubs.monitoring] %s at %s (%s)", c.hub.Fort, c.hubIP, where)})

	// Orphans: a file in the hub's directory for a fort with no manifest.
	entries, _ := os.ReadDir(filepath.Join(c.root, hubDir(c.hub.Fort)))
	for _, e := range entries {
		if e.Name() == "kustomization.yaml" || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		if slices.Contains(c.forts, name) || anotherRolesFile(name, c.forts) {
			continue
		}
		out = append(out, kit.Finding{Level: kit.Error, Dir: hubFilePath(c.hub.Fort, name), Msg: "no manifest in " + kit.CoreFortsDir + "/ — a retired fort's rules would page for a cluster that is gone; fort monitoring dismantle " + name + " removes this file"})
	}
	// The directory builds: the estate's own clusters never apply it
	// directly, so nothing else in the gate would — and a file that does
	// not build fails on the hub, in its flux.
	if kit.Exists(c.root, hubKustomization(c.hub.Fort)) {
		out = append(out, c.buildFinding(hubDir(c.hub.Fort), "what the hub's subscription applies", "the hub's subscription would fail on this directory"))
	}
	// The push, once any cluster fort exists.
	if anyCluster && c.est.PushMetrics() {
		patch := filepath.Join(monitoringConfigs, pushFile)
		kust := filepath.Join(monitoringConfigs, "kustomization.yaml")
		raw, _ := os.ReadFile(filepath.Join(c.root, kust))
		switch {
		case !kit.Exists(c.root, patch):
			out = append(out, kit.Finding{Level: kit.Error, Dir: patch, Msg: "missing — [push] metrics is on and the edge Prometheus remote-writes nowhere; fort monitoring scaffold <fort> renders it once"})
		case !strings.Contains(string(raw), "path: "+pushFile):
			out = append(out, kit.Finding{Level: kit.Error, Dir: kust, Msg: "does not list " + pushFile + " under patches — the patch is there and kustomize never applies it; fort monitoring scaffold <fort> appends it"})
		default:
			out = append(out, kit.Finding{Level: kit.OK, Dir: patch, Msg: "the edge Prometheus remote-writes to http://${" + hubVar + "}:30990, listed"})
		}
	}
	if anyCluster && c.est.PushLogs() {
		release := filepath.Join(fluentBitDir, "release.yaml")
		switch {
		case !kit.Exists(c.root, release):
			out = append(out, kit.Finding{Level: kit.Error, Dir: fluentBitDir, Msg: "missing — [push] logs is on and nothing ships them; fort monitoring scaffold <fort> renders the controller once"})
		case !kit.KustomizationLists(c.root, controllersDir, fluentBitName):
			out = append(out, kit.Finding{Level: kit.Error, Dir: filepath.Join(controllersDir, "kustomization.yaml"), Msg: "does not list " + fluentBitName + " — the controller is there and flux never applies it; fort monitoring scaffold <fort> appends it"})
		default:
			out = append(out, kit.Finding{Level: kit.OK, Dir: fluentBitDir, Msg: "fluent-bit ships to http://${" + hubVar + "}:31100, listed"})
		}
	}
	return out
}

// edgeFileNames is the edge stack's files, by name.
func edgeFileNames() []string {
	var out []string
	for _, f := range edgeFiles {
		out = append(out, f.name)
	}
	return out
}

// hubDeclarationFindings: the two ends of the hub agree. The fort the
// core names its hub carries [monitoring.hub]; no other fort does.
func (c checkCtx) hubDeclarationFindings(declared []string) []kit.Finding {
	var out []kit.Finding
	if c.hasHub && c.core.hubLocal(c.hub) && slices.Contains(c.forts, c.hub.Fort) {
		if hubOf(c.root, c.hub.Fort) == nil {
			out = append(out, kit.Finding{Level: kit.Error, Dir: role.FortPath(c.hub.Fort), Msg: "no [monitoring.hub] — estate.toml names " + c.hub.Fort + " this estate's monitoring hub and the stack renders from that table; fort monitoring scaffold " + c.hub.Fort + " writes the shape"})
		}
	}
	for _, f := range declared {
		if hubOf(c.root, f) != nil && !c.core.isHubFort(f) {
			out = append(out, kit.Finding{Level: kit.Error, Dir: role.FortPath(f), Msg: "[monitoring.hub] on a fort estate.toml does not name as its [hubs.monitoring] — the core declares the hub, the layer renders it (forts' docs/hub.md)"})
		}
	}
	return out
}

// buildFinding runs kustomize on a directory when the tool is there
// (the role binary carries it; a bare `go test` may not).
func (c checkCtx) buildFinding(dir, ok, bad string) kit.Finding {
	cmd, err := tools.Command("kustomize", "build", filepath.Join(c.root, dir))
	if err != nil {
		return kit.Finding{Level: kit.Warning, Dir: dir, Msg: "not built: " + err.Error()}
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if _, err := cmd.Output(); err != nil {
		return kit.Finding{Level: kit.Error, Dir: dir, Msg: "kustomize build: " + kit.LastLine(stderr.String()) + " — " + bad}
	}
	return kit.Finding{Level: kit.OK, Dir: dir, Msg: "kustomize build (" + ok + ")"}
}

// fortFindings is one fort: its declaration, its file for the hub, its
// cluster's HUB_MESH_IP, and the hub's renders when it serves the role.
func (c checkCtx) fortFindings(fort string) []kit.Finding {
	if !kit.ValidFortName(fort) {
		return []kit.Finding{{Level: kit.Error, Dir: kit.CoreFortsDir, Msg: fmt.Sprintf("%q is not a fort name", fort)}}
	}
	if !slices.Contains(c.forts, fort) {
		return []kit.Finding{{Level: kit.Error, Dir: filepath.Join(kit.CoreFortsDir, fort+".toml"), Msg: "no such fort in the core"}}
	}
	if !c.hasHub {
		return []kit.Finding{{Level: kit.OK, Dir: role.FortPath(fort), Msg: "no hub to render for (estate.toml names no [hubs.monitoring]); the edge stack is the estate finding"}}
	}
	if c.hubErr != nil {
		return nil // said once, in the estate findings
	}
	var out []kit.Finding
	path := role.FortPath(fort)
	var hub *Hub
	if !kit.Exists(c.root, path) {
		out = append(out, kit.Finding{Level: kit.Error, Dir: path, Msg: "missing — no fort of an estate with a hub goes unwatched; fort monitoring scaffold " + fort + " writes it"})
	} else if f, err := loadFort(c.root, fort); err != nil {
		out = append(out, kit.Finding{Level: kit.Error, Dir: path, Msg: strings.TrimPrefix(err.Error(), path+": ")})
	} else {
		hub = f.Monitoring.Hub
	}
	cf, err := loadCoreFort(c.root, fort)
	if err != nil {
		return append(out, kit.Finding{Level: kit.Error, Dir: filepath.Join(kit.CoreFortsDir, fort+".toml"), Msg: err.Error()})
	}
	what, note := "a plain fort the hub does not scrape", plainNote
	if cf.IsCluster() {
		what, note = "a flux cluster with no dead man's switch for the hub", clusterNote
	}
	rel := hubFilePath(c.hub.Fort, fort)
	kust, _ := os.ReadFile(filepath.Join(c.root, hubKustomization(c.hub.Fort)))
	switch {
	case !kit.Exists(c.root, rel):
		out = append(out, kit.Finding{Level: kit.Error, Dir: rel, Msg: "missing — " + fort + " is " + what + "; fort monitoring scaffold " + fort + " renders it"})
	case !kit.ListsResource(string(kust), fort+".yaml"):
		out = append(out, kit.Finding{Level: kit.Error, Dir: hubKustomization(c.hub.Fort), Msg: "does not list " + fort + ".yaml — the file exists and the hub's flux never applies it; fort monitoring scaffold " + fort + " adds the line"})
	default:
		out = append(out, kit.Finding{Level: kit.OK, Dir: rel, Msg: note})
	}
	if cf.IsCluster() {
		vars := clusterVarsPath(fort)
		raw, err := os.ReadFile(filepath.Join(c.root, vars))
		switch {
		case err != nil:
			out = append(out, kit.Finding{Level: kit.Warning, Dir: vars, Msg: "not rendered yet (fort scaffold " + fort + ") — nothing to check for " + hubVar})
		default:
			got, set := clusterVar(string(raw), hubVar)
			switch {
			case !set:
				out = append(out, kit.Finding{Level: kit.Error, Dir: vars, Msg: "no " + hubVar + " — the shared layer's push references it and flux's strict substitution fails the whole layer without it; fort monitoring scaffold " + fort + " adds it"})
			case got != c.hubIP:
				out = append(out, kit.Finding{Level: kit.Error, Dir: vars, Msg: fmt.Sprintf("%s is %s, and the hub's address on this mesh is %s — the edge pushes into nothing; the file is the estate's, correct the line", hubVar, got, c.hubIP)})
			default:
				out = append(out, kit.Finding{Level: kit.OK, Dir: vars, Msg: hubVar + " " + got})
			}
		}
	}
	if hub != nil && c.core.isHubFort(fort) {
		out = append(out, c.hubFindings(fort, hub)...)
	}
	return out
}

// hubFindings: what the hub's table promises is in the tree.
func (c checkCtx) hubFindings(fort string, hub *Hub) []kit.Finding {
	var out []kit.Finding
	scaffold := "fort monitoring scaffold " + fort
	// The stack.
	stack := stackDir(fort)
	apps := filepath.Join(appsDir(fort), "kustomization.yaml")
	switch {
	case !kit.Exists(c.root, filepath.Join(stack, "kustomization.yaml")):
		out = append(out, kit.Finding{Level: kit.Error, Dir: stack, Msg: "missing — [monitoring.hub] promises the central stack; " + scaffold + " renders it"})
	case !kit.KustomizationReaches(c.root, appsDir(fort), stackName):
		out = append(out, kit.Finding{Level: kit.Error, Dir: apps, Msg: "does not reach " + stackName + " — no entry is the stack or includes it, so flux never applies it; " + scaffold + " appends it (a second layer's overlay entry counts when its kustomization includes the stack, #4)"})
	default:
		var missing []string
		for _, f := range stackFileNames() {
			if !kit.Exists(c.root, filepath.Join(stack, f)) {
				missing = append(missing, f)
			}
		}
		legacy := false
		if msg, old := kit.LegacyInventory(c.root, stack); old {
			legacy = true
			out = append(out, kit.Finding{Level: kit.Error, Dir: filepath.Join(stack, kit.LegacyInventoryFile), Msg: msg})
			missing = slices.DeleteFunc(missing, func(f string) bool { return f == kit.InventoryFile })
		}
		if len(missing) > 0 {
			out = append(out, kit.Finding{Level: kit.Error, Dir: stack, Msg: "lacks " + strings.Join(missing, ", ") + " — " + scaffold + " fills the gap"})
		} else if !legacy {
			out = append(out, kit.Finding{Level: kit.OK, Dir: stack, Msg: "the central stack: grafana." + hub.Hostname + "; retention " + hub.RetMetrics() + ", logs " + hub.RetLogs()})
		}
		var absent []string
		for _, s := range stackSecrets {
			if !kit.Exists(c.root, filepath.Join(stack, s)) {
				absent = append(absent, s)
			}
		}
		if len(absent) > 0 {
			out = append(out, kit.Finding{Level: kit.Warning, Dir: stack, Msg: "no " + strings.Join(absent, ", ") + " yet — listed, so kustomize fails until it exists; fort monitoring mint " + fort + " --write"})
		} else {
			out = append(out, c.buildFinding(stack, "the stack", "flux would fail the apps layer on it"))
		}
	}
	// The subscription. Files beside it that this layer did not render
	// are another layer's (its own check owns them), so only the own
	// estate's is judged here.
	for _, s := range serves(c.core, fort) {
		rel := servesPath(fort, s.Name)
		switch {
		case !kit.Exists(c.root, rel):
			out = append(out, kit.Finding{Level: kit.Error, Dir: rel, Msg: "missing — the hub applies nothing of this estate without its subscription; " + scaffold + " renders it"})
		case !kit.KustomizationLists(c.root, servesDir(fort), s.Name+".yaml"):
			out = append(out, kit.Finding{Level: kit.Error, Dir: filepath.Join(servesDir(fort), "kustomization.yaml"), Msg: "does not list " + s.Name + ".yaml — the subscription exists and flux never applies it; " + scaffold + " adds the line"})
		case !kit.KustomizationLists(c.root, appsDir(fort), servesName):
			out = append(out, kit.Finding{Level: kit.Error, Dir: apps, Msg: "does not list " + servesName + " — the subscription exists and flux never applies it; " + scaffold + " adds the line"})
		default:
			out = append(out, kit.Finding{Level: kit.OK, Dir: rel, Msg: "this estate's own forts: " + hubDir(fort) + "/ through the flux-system source"})
		}
	}
	// The gateway.
	gw := gatewayDir(fort)
	gkust, err := os.ReadFile(filepath.Join(c.root, gw, "kustomization.yaml"))
	if err != nil {
		out = append(out, kit.Finding{Level: kit.Error, Dir: gw, Msg: "no Gateway here — the stack's route attaches to the fort's Gateway (a gateway battery renders it)"})
	} else {
		var gaps []string
		for _, g := range gatewayNames {
			cert, listener := "certificate-"+g.name+".yaml", "listener-"+g.name+".yaml"
			switch {
			case !kit.Exists(c.root, filepath.Join(gw, cert)):
				gaps = append(gaps, cert+" missing")
			case !kit.ListsResource(string(gkust), cert):
				gaps = append(gaps, cert+" unlisted")
			case !kit.Exists(c.root, filepath.Join(gw, listener)):
				gaps = append(gaps, listener+" missing")
			case !strings.Contains(string(gkust), "path: "+listener):
				gaps = append(gaps, listener+" not under patches")
			}
		}
		if len(gaps) > 0 {
			out = append(out, kit.Finding{Level: kit.Error, Dir: gw, Msg: strings.Join(gaps, ", ") + " — " + scaffold + " appends them"})
		} else {
			out = append(out, kit.Finding{Level: kit.OK, Dir: gw, Msg: "grafana." + hub.Hostname + ": certificate and listener"})
		}
	}
	// The host.
	hostNix := hostNixPath(fort)
	raw, err := os.ReadFile(filepath.Join(c.root, hostNix))
	switch {
	case err != nil:
		out = append(out, kit.Finding{Level: kit.Error, Dir: hostNix, Msg: "missing — fort scaffold " + fort + " renders it, then " + scaffold + " opens the ports"})
	case !strings.Contains(string(raw), ".nixosModules.monitoring-hub") || !kit.NixSetsAttr(string(raw), "sfi.monitoring-hub"):
		out = append(out, kit.Finding{Level: kit.Error, Dir: hostNix, Msg: fmt.Sprintf("the host imports no monitoring-hub module or sets no sfi.monitoring-hub — %d and %d stay closed on the mesh; %s", portMetrics, portLogs, scaffold)})
	default:
		out = append(out, kit.Finding{Level: kit.OK, Dir: hostNix, Msg: fmt.Sprintf("sfi.monitoring-hub: %d, %d on the mesh interface", portMetrics, portLogs)})
	}
	return out
}

// clusterVar reads one key of a cluster-vars ConfigMap's data, by line.
func clusterVar(text, key string) (string, bool) {
	for _, l := range strings.Split(text, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, key+":") {
			return strings.TrimSpace(strings.TrimPrefix(t, key+":")), true
		}
	}
	return "", false
}

// anotherRolesFile says a file in the hub's directory is a fort's file
// from another role — <fort>-<role>.yaml, a backups role's
// PVBackupNotConfigured beside the fort's own file — and not an orphan:
// that role's check owns it.
func anotherRolesFile(name string, forts []string) bool {
	for _, f := range forts {
		if strings.HasPrefix(name, f+"-") {
			return true
		}
	}
	return false
}
