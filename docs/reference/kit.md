# The kit — what the role binaries share

`internal/kit` is the one package every role of this repo builds on. It
holds what the core's verbs hold and a layer cannot import: the core's
`internal/` is closed, and a public Go API is a decision after v0.1
(forts#144). So the helpers are **copied** from sfi/forts `cmd/fort` at
`56ee5cd`, the commit before forts#143 removed the layers from the
core, and exported. A helper that changes here changes for every role;
a helper that changes in the core does not change here. That is the
point of the copy, and the price of it: a fix in one place is a fix in
one place.

## What is in it

| file | from sfi/forts | what |
|---|---|---|
| `kit.go` | `scaffold.go` repoRoot, `layer.go` | `Root()`: `FORT_ESTATE_ROOT` when the core execs the binary, else the nearest `.git` upward — and either way a directory with an `estate.toml` |
| `layer.go` | `internal/manifest` Load/LoadAll/Names, `internal/estate` Load | `Layer`: the role's paths under `layers/<role>/`, strict loaders for its `estate.toml` and `forts/<fort>.toml`, `CoreForts` (the names the core declares), `CoreEstate` (the core's file, read loosely for the role's own entries) |
| `plan.go` | `add.go` action/plan, `layer.go` layerPlan | `Plan`: `New`, `Append`, `Remove`, then `Apply`, `Stage`, `Report` in the marked-tree shape (● ○ ◆ ✗ ·) |
| `finding.go` | `check.go` finding/reportFindings | `Finding`, `Report`: ✓ ! ✗, the workflow commands under Gitea Actions, and an unknown level is an error |
| `sops.go` | `sops.go` | `Secrets` and `Sops`: decrypt, encrypt in place, set, unset, updatekeys, under the estate's config or an explicit one |
| `yamledit.go` | `yamledit.go` | append a list item, a map entry, a patch, a block — text edits that keep every comment |
| `tomledit.go` | `tomledit.go` | set a key in a table, append a table, quote a string |
| `nixedit.go` | `nixedit.go` | ensure an import, ensure a block, say whether a line sets an attribute |
| `../tools` | `internal/tools` | the declared tool set (forts#25): `tools.json`, `Ensure`, `Command`, `Runner`, and the test that reads the source for an exec that went around it |

Not copied: the core's verbs and their types (mint's plan, scaffold's
views, check's contract gate), the manifest's provider tables, the
estate's hub and link logic. A role reads the core's files for the few
keys it needs through `CoreEstate` and `CoreForts`, and writes none of
them.

## How a role uses it

A role is `cmd/fort-<role>/main.go`: a verb switch over `init`,
`scaffold`, `mint`, `check`, `dismantle` (forts' docs/layers.md gives
the order against the core's verbs), each in the shape of the core's:

- an **author verb** (`init`, `scaffold`, `mint`, `dismantle`) builds a
  `kit.Plan`, prints it, and with `--write` applies and stages it;
- the **check** builds `kit.Finding`s per heading and exits non-zero on
  an error, with `GITEA_ACTIONS` turning errors and warnings into
  workflow commands;
- the role's files are `layers/<role>/estate.toml` and
  `layers/<role>/forts/<fort>.toml`, loaded strictly into the role's
  own structs — a key the struct lacks is a refusal, not a default.

`cmd/fort-backups` is the first role in this shape (#1 proved it as a
stub, #3 filled it): docs/backups.md.

## The flake

`flake.nix` builds every `cmd/fort-*` directory as `packages.fort-<role>`,
wrapped with the tool set on PATH, and exports every
`modules/<role>/<name>.nix` as `nixosModules.<role>-<name>`. An estate
pins this flake once (`fort layer add <url>#<role>`), and the gate runs
`nix run ./nixos#fort-<role> -- check --all` at that pin.
