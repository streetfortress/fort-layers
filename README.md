# Fort Layers

Add infrastructure capabilities to your estates programatically with _fort layers_

[Forts!](https://github.com/streetfortress/forts) is opinionated enough about its baseline: NixOS machines running k3s distributed across a wireguard mesh. When designing forts as a framework, we originally wanted to go "batteries included" with it, but realized that not everybody would want to use eg the same monitoring stack used by SFI internally. So the "batteries" have been abstracted out into `fort-layers`: 

> A "fort layer" is a program structurally similar to the main `fort` CLI. It follows the same 'scaffold, mint, merge, dismantle' lifecycle, and allows you to programatically configure **both nixos AND workloads** across an estate.

For example, the `monitoring` layer contained herein adds

- Node exporter as a system unit
- An "edge prometheus" stack, facilitating observability requirements even when the mesh is down.
- A "hub" stack on a centralized fort: Prometheus remote-write configurations across the estate to a central instance + grafana and dashboard json

By implementing this as a "fort layer", we can now configure ALL of our estates to have a uniform observability surface. You can extend this idea to any service or infrastructure dependency (databases, cache, s3, backups, analytics... the list goes on).

## The Shape of `fort-layers`

```
cmd/fort-<role>/      one go binary per role; the flake exports each as packages.fort-<role>
internal/kit/         what the roles share, copied from sfi/forts (docs/reference/kit.md)
internal/tools/       the declared tool set the wrappers carry on PATH
modules/<role>/       the NixOS modules a role ships, as nixosModules.<role>-<name>
docs/                 the hand-written introduction; docs/reference/ moves
                      with the code — one page per role, and the kit's
```

A role reads `layers/<role>/estate.toml` and `layers/<role>/forts/<fort>.toml`
in an estate, and writes no file of the core's. `nix develop` gives the
Go toolchain and the tools; `go test ./...` is what CI runs on the
checkout, and `nix build .#fort-<role>` is the binary an estate pins.

## Roles

| role | binary | docs | state |
|---|---|---|---|
| monitoring | `fort-monitoring` | [docs/reference/monitoring.md](docs/reference/monitoring.md) | A production ready observability surface for estates |

