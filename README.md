# Fort Layers

Add infrastructure capabilities to your environments with _fort layers_

## Shape

```
cmd/fort-<role>/      one binary per role; the flake exports each as packages.fort-<role>
internal/kit/         what the roles share, copied from sfi/forts (docs/kit.md)
internal/tools/       the declared tool set the wrappers carry on PATH
modules/<role>/       the NixOS modules a role ships, as nixosModules.<role>-<name>
docs/                 one page per role, and the kit's
```

A role reads `layers/<role>/estate.toml` and `layers/<role>/forts/<fort>.toml`
in an estate, and writes no file of the core's. `nix develop` gives the
Go toolchain and the tools; `go test ./...` is what CI runs on the
checkout, and `nix build .#fort-<role>` is the binary an estate pins.

## Roles

| role | binary | docs | state |
|---|---|---|---|
| monitoring | `fort-monitoring` | [docs/monitoring.md](docs/monitoring.md) | the reference layer: the edge stack on every cluster fort, and a one-estate hub |
