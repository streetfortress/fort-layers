# Fort Layers — Getting Started (WIP)

[Forts](https://github.com/streetfortress/forts) gives you an estate: the machines and the reconcilers, and deliberately nothing more. A **layer** is how an estate grows a capability beyond that — monitoring, backups, your own — without the framework having an opinion about it.

A layer is three concrete things:

- **A binary** your estate pins by revision, exactly like `fort` itself. This repo's flake exports one per role (`packages.fort-monitoring`), and `fort monitoring <verb>` execs it.
- **Files in your estate repo.** A layer owns `layers/<role>/` (its estate-wide settings and one file per fort) and renders flux configuration alongside the core's — appends and new files, never a rewrite of something the core manages.
- **A gate.** Every layer ships a `check`, and your estate's CI runs it after the core's. A layer you installed is a layer your gate enforces.

The layer *protocol* — what makes all of this uniform — lives in forts' own reference: [docs/reference/layers.md](https://github.com/streetfortress/forts/blob/main/docs/reference/layers.md). This repo is the layers themselves, and `monitoring` is the reference layer: read it to learn the shape, fork nothing to use it.

## Prerequisites

- A working estate built with forts ([getting started](https://github.com/streetfortress/forts/blob/main/docs/README.md)).
- That's it. The layer binaries ride the same toolchain the estate already has.

## Install the monitoring layer

**1. Pin the layer in your estate's flake.** Name the input after this repo:

```nix
fort-layers.url = "github:streetfortress/fort-layers/<tag-or-rev>";
fort-layers.inputs.nixpkgs.follows = "nixpkgs";
```

and put `fort-layers.packages.<system>.fort-monitoring` in the estate flake's `layers` set, so the binary is on `fort`'s PATH at the pinned revision.

**2. Initialize the layer.**

```sh
fort layer add github:streetfortress/fort-layers#monitoring --write -- --write
```

This runs the layer's `init`, which writes `layers/monitoring/estate.toml`: what every cluster fort pushes to the hub (metrics and logs, both on by default). No hub yet? That's fine — the layer says so and the edge stack still works alone.

**3. Scaffold each fort.**

```sh
fort monitoring scaffold <fort> --write
```

Every cluster fort gets the **edge stack**: an edge Prometheus and Alertmanager, the node-exporter scrape seam, the "this cluster is unhealthy" rules, and the controllers they need. The edge stack is self-contained — it evaluates and pages even when the rest of your estate is unreachable.

**4. Name a hub (optional, recommended).** One fort — this estate's or another's, reached through a link — gets the central view. Declare it in the core's `estate.toml`:

```toml
[hubs.monitoring]
estate = "ssh://git@<your forge>/<owner>/<estate>"   # the estate that holds the hub fort
fort = "central"
```

Re-run scaffold: every watched fort now renders its own dead man's switch into `flux/hub/<hub>/`, and the hub fort renders the central stack — Prometheus receiving every edge's push, Loki, Grafana, a central Alertmanager. Then mint the one credential:

```sh
fort monitoring mint <hub> --write
```

**5. Add the gate.** Your estate's CI runs the core's check already; add the layer's after it:

```sh
fort monitoring check --all
```

**6. Route alerts somewhere.** Out of the box, alerts land in the Alertmanager UI and nowhere else — the shipped config is plaintext and committed, so a routing change is a readable PR diff. A notifier (Discord, Slack, any webhook) is one sops secret and one edit, documented in the rendered config file itself. Until you add one, nothing is misconfigured: a fresh estate reconciles green with zero hand-written files.

The full render tables, the check's findings, and the Alertmanager design are in [reference/monitoring.md](reference/monitoring.md).

## Extending a layer

A layer is built to be included, not forked. The monitoring hub's stack has a declared swap point: an overlay directory can include it through kustomize and patch it — add a Thanos tier, swap Loki onto an object store, serve more estates — and the layer's own check accepts that shape. Your overlay is itself a layer, with its own binary, its own files, its own check running after this one's.

The seams are documented in [reference/monitoring.md](reference/monitoring.md), "A second layer". We use them ourselves: SFI's internal estates run an overlay on this exact base.

## Writing your own layer

Start from the protocol ([forts' layers.md](https://github.com/streetfortress/forts/blob/main/docs/reference/layers.md)) and this repo's [kit](reference/kit.md) — the helpers the role binaries share. A role is one binary, one directory in the estate (`layers/<role>/`), renders that append and never rewrite, and a check the estate's gate runs. The monitoring role is the worked example for all of it.
