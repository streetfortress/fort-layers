# monitoring — the edge stack, the hub, and the forts it watches

The monitoring role of fort-layers: the reference layer (#2, #3). The
binary is `fort-monitoring`; an estate runs it as `fort monitoring
<verb>` (forts' docs/reference/layers.md). forts core ships no
observability opinion (forts#270) — this layer is that opinion, in two
halves:

- **The edge stack**, on every cluster fort: an edge Prometheus and
  Alertmanager, node-exporter's scrape seam, the "this cluster is
  unhealthy" rules, the mesh NodePorts, and the three controllers they
  need. Self-contained: it evaluates and pages with the mesh down, and
  an estate with no hub still has it.
- **The hub**, on the one fort `[hubs.monitoring]` names: a central
  Prometheus that receives every edge's push and evaluates the fleet
  rules, Loki for the logs, Grafana, a central Alertmanager. The watched
  side renders one file per fort into `flux/hub/<hub>/`, and the hub
  applies that directory through a flux subscription — a hub is a
  relationship between the estate and one fort (forts#38), and
  `[hubs.monitoring]` can name a fort in another estate, reached through
  a link.

The core keeps the declaration: `estate.toml` names the hub fort and its
estate in `[hubs.monitoring]` (forts' docs/hub.md). This role reads it
and writes none of the core's files. The core's `[[serves.<role>]]`
entries — a hub that serves OTHER estates — are not this layer's: a
layer built on this one owns the cross-estate subscriptions, the link
ports and an object-store tier (see "A second layer" below).

## The two files

**`layers/monitoring/estate.toml` holds what the layer decides for the
estate**: what every cluster fort pushes. Both streams are on unless a
line says otherwise. `fort monitoring init` renders it.

```toml
[push]
metrics = true   # the edge Prometheus remote-writes to the hub (its NodePort 30990 on the mesh)
logs = true      # fluent-bit ships container, journal and audit logs to the hub's Loki (31100)
```

**`layers/monitoring/forts/<fort>.toml` is required for every fort the
core declares, once the core names a hub.** `[monitoring]` with nothing
under it is the positive declaration that the hub watches this fort;
scaffold writes it when it is not there. With no hub in the core there
are no fort files: the edge stack is the whole render.

```toml
[monitoring]
```

**The fort that serves the role adds `[monitoring.hub]`**, and the
central stack renders from it. The core says which fort that is; the
table says how. A hub fort with no file yet gets it from scaffold with
this shape and the defaults, and scaffold stops there: the operator
refines the hostname and the retention, and the next run renders the
stack. A table on a fort the core names no hub, or a hub fort with no
table, is an error at both verbs.

```toml
[monitoring.hub]
hostname = "central.example.org"   # grafana.<hostname> on the fort's Gateway

[monitoring.hub.retention]         # the hub's history, on the fort's disk
metrics = "30d"
logs = "30d"
```

## The verbs

| verb | what it does |
|---|---|
| `init [--no-metrics] [--no-logs]` | renders the estate file, both streams on unless a flag turns one off. Says when the core names no hub yet. `fort layer add <url>#monitoring` runs it. |
| `scaffold <fort>` | renders the fort's side of the layer (below). Appends and new files, never a rewrite; a re-run fills gaps. |
| `check [--all \| <fort>]` | the layer's gate: the estate's gate runs it after the core's. |
| `mint <hub>` | the stack's credential, after scaffold: grafana-admin. A file that exists is kept. |
| `dismantle <fort>` | runs before the core's dismantle: removes the fort's file for the hub and its line, and the fort's declaration. The hub drops the fort's rules by prune. |

**What `scaffold` renders.** The hub's address on this mesh comes from
the core: the hub fort's manifest (`mesh_ip`) when the hub is a fort of
this estate, its `remotes` entry in `mesh/peers.json` when it is
another's — the link that puts it on this mesh (forts' docs/mesh.md). A
missing entry is refused by name, not rendered empty.

| the fort | renders |
|---|---|
| a cluster fort, once per estate | `● flux/infrastructure/configs/monitoring/`: the edge stack — the edge Prometheus (24h local, `cluster` and `estate` stamped on every series), the edge Alertmanager and its **committed plaintext config** (below), node-exporter's Service/Endpoints seam (the host runs node_exporter at the NixOS level, forts' metrics.nix; nothing scrapes it without this layer), the ServiceMonitors and PodMonitors, the edge rules, NodePorts 30909/30993, the labelled ops read rule (forts#169); `○ configs/kustomization.yaml` + `monitoring` |
| a cluster fort, once per estate | `● flux/infrastructure/controllers/{prometheus-operator-crds,prometheus-operator,kube-state-metrics}/`; `○ controllers/kustomization.yaml` + each |
| any, with a hub | `● layers/monitoring/forts/<fort>.toml` when absent |
| a cluster fort, with a hub | `● flux/hub/<hub>/<fort>.yaml`: `ClusterStreamSilent` (`absent(up{cluster="<fort>"})` — the only expression that can fire when a cluster goes silent), `LogStreamSilent` as a Loki rule, and its edge Alertmanager as a Grafana datasource (over the mesh, NodePort 30993; the hub's own edge through cluster DNS). Every rule carries `cluster` and `estate`. |
| a plain fort, with a hub | `● flux/hub/<hub>/<fort>.yaml`: a `ScrapeConfig` for node-exporter over the mesh, `job="host-node-exporter"`, and `HostDown` on it. |
| any, with a hub | `○ flux/hub/<hub>/kustomization.yaml` + `<fort>.yaml` (`●` on the estate's first fort) |
| a cluster fort, with a hub | `○ flux/clusters/<fort>/cluster-vars.yaml` + `HUB_MESH_IP` |
| a cluster fort, `[push] metrics`, once per estate | `● flux/infrastructure/configs/monitoring/push-metrics.yaml`: a kustomize patch that adds `remoteWrite` to the edge Prometheus; the `patches:` entry rides in the directory's kustomization. The rendered CR is never edited. |
| a cluster fort, `[push] logs`, once per estate | `● flux/infrastructure/controllers/fluent-bit/` (container, journal and audit streams, the estate's label set); `○ controllers/kustomization.yaml` + `fluent-bit` |

**On the fort that serves the role**, from its `[monitoring.hub]`:

| renders | what |
|---|---|
| `● flux/apps/<hub>/monitoring-central/` | the central stack: the namespace `monitoring-central` (apart from the edge's `monitoring`, so the operator's per-namespace services cannot fan every edge alert into both); the central Prometheus — remote-write receiver, the history at the table's retention, and the fleet-rule evaluation (`monitoring.sfi/scope: central`, the label every edge excludes); the central Alertmanager and its committed config; the two mesh NodePorts; Grafana at `grafana.<hostname>`, the edge Alertmanagers arriving as datasources; Loki on the filesystem with the OTLP label set; the log rules; blackbox, with no targets; the central ServiceMonitors; `forts.inventory.yaml`. `grafana-admin.sops.yaml` is listed and absent until mint. |
| `● flux/apps/<hub>/serves/<estate>.yaml` | the subscription: `flux/hub/<hub>/` of this repo through the flux-system source, applied as a ServiceAccount that writes PrometheusRule, ScrapeConfig and ConfigMap in `monitoring-central` and nothing else |
| `○ flux/apps/<hub>/kustomization.yaml` | + `monitoring-central`, + `serves` |
| `○ flux/apps/<hub>/gateway/` | `certificate-grafana.yaml`, `listener-grafana.yaml`; the issuer is the one the Gateway's other certificates name (`letsencrypt` when there is none) |
| `○ nixos/hosts/<hub>/default.nix` | the module `monitoring-hub` (`modules/monitoring/hub.nix`) and `sfi.monitoring-hub.enable = true`: 30990 and 31100 on the mesh interface |

## The Alertmanager config is committed, the webhook is opt-in (#3)

Both Alertmanagers (edge and central) read a **plaintext Secret
committed in the repo** — `alertmanager-edge` / `alertmanager-central`,
key `alertmanager.yaml`. Nothing in the default is a secret, so a route
change is a readable PR diff, and the operator's naming trap (a Secret
under any other name is silently ignored) is gone: the template writes
the name the operator reads.

The default route delivers to a receiver with no integration: alerts
land in the Alertmanager's UI (the edge's on mesh NodePort 30993;
central's in Grafana's Silences pane too), and a fresh estate reconciles
green with zero hand-written files.

A notifier is one secret and one edit, documented in the rendered
config file itself: a sops Secret with a single `url` key, mounted via
`spec.secrets` (the mount is part of the opt-in — a named-but-missing
Secret blocks the pod), and a receiver that reads it with `url_file`.
`webhook_configs` is the shipped example; Discord (`webhook_url_file`)
and Slack (`api_url_file`) are one-line variants. For the edge config
the secret lives under the shared infrastructure sops rule — the
fort-managed rule (forts#271), so every cluster's recipient is on it and
the core's mint/dismantle keep it current. For the central config it
lives under the hub fort's own rule.

**What `mint <hub>` writes.** One file: `grafana-admin.sops.yaml`
(`admin` and a generated password). `forts.inventory.yaml` in the stack
directory is the record.

**What `check` reads.** The tree alone.

The output follows the core's `fort check` (forts#268): success is terse,
failure is verbose. An info line is a fact (`push`, `hub`) with no mark,
and it cannot fail. A `✓` marks a check that could have failed, with a
short message. A heading with no findings prints no header. Error and
warning lines do not change.

| finding | level |
|---|---|
| the estate file or a fort file does not parse | error |
| a fort file for a fort the core does not declare | error |
| the edge stack missing a file, or unlisted; a controller missing or unlisted | error |
| `ops-readonly.yaml` carries no `ops-ro-layers-read` | error: the layer's labelled read rule has no taker (an estate rendered before forts#169 adds it by hand) |
| no `[hubs.monitoring]` in the core | info, said once (`hub  none`): no push, no file, the edge Alertmanager is the only alerting |
| the hub's address on this mesh does not resolve | error |
| a core fort with no declaration, or no file in `flux/hub/<hub>/`, or a file the directory does not list | error: a fort with no dead man's switch is the silent failure this layer exists to catch |
| a file in `flux/hub/<hub>/` for a fort with no manifest | error: a retired fort's rules would page for a cluster that is gone |
| `flux/hub/<hub>/` does not build | error (a warning when kustomize is not on PATH) |
| a cluster's `HUB_MESH_IP` is missing, or is not the hub's address | error: the edge pushes into nothing |
| `[push]` declares a stream and its render is missing or unlisted | error |
| the hub fort has no `[monitoring.hub]`; a fort with the table that the core does not name | error |
| the stack missing, lacking a file, or not building (the build waits for the credential); the apps kustomization does not reach it — no entry is the stack or includes it in its own kustomization (#4) | error |
| the subscription missing or unlisted | error |
| the gateway lacks the certificate or the listener, or does not list or patch them | error |
| the host imports no `monitoring-hub` module or sets no `sfi.monitoring-hub` | error |
| `grafana-admin.sops.yaml` absent | warning: mint is the next step |

## A second layer

This layer is built to be included, not forked. The seams, for a layer
that adds an object-store tier, cross-estate subscriptions, or a
dashboards distribution:

- The swap point is the apps kustomization: its entry moves from
  `monitoring-central` to an overlay directory that includes it through
  kustomize and patches it. This layer's check accepts that shape (#4):
  the entry counts when its own kustomization includes the stack, one
  hop deep — what else the overlay holds is the second layer's own
  check's work. A Thanos sidecar is a patch on the central
  Prometheus CR; a query tier repoints Grafana's datasource url and
  keeps the uid (`fenqsyb8rfke8c` / `cenug0ko9f4zkd` — the contract a
  dashboard names).
- Loki's storage block swaps from `filesystem` to an object store the
  same way.
- `serves/` takes more files: one subscription per estate served across
  repos. This layer's check judges only its own.
- The push patch is the precedent for all of it: a file of your own
  beside a rendered one, never an edit.

## Not in this layer

- The dashboards' content: the stack ships the sidecar wiring
  (`grafana_dashboard`-labelled ConfigMaps) and no dashboards yet — a
  bundled set is a separate issue.
- The push carries no authentication (#9): the hub trusts the
  identity labels the mesh peers stamp. The mesh is the boundary.
- An object store, long retention tiers, multi-estate tenancy: a second
  layer's (above).
