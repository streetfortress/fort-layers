tag version:
  #!/usr/bin/env bash
  set -euo pipefail
  v="{{version}}"
  v="${v#v}"
  [[ "$v" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] \
      || { echo "error: 'v$v' is not vX.Y.Z semver" >&2; exit 1; }
  git fetch origin --tags --prune --prune-tags
  [[ "$(git rev-parse HEAD)" == "$(git rev-parse origin/main)" ]] \
      || { echo "error: HEAD is not at origin/main — pull first" >&2; exit 1; }
  git tag "v$v" && git push origin "v$v"
