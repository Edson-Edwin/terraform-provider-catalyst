#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

cat > "$repo_dir/.terraformrc.local" <<EOF
provider_installation {
  dev_overrides {
    "registry.terraform.io/edson-edwin/catalyst" = "$repo_dir/bin"
  }
  direct {}
}
EOF

echo "Created project-local development override: $repo_dir/.terraformrc.local"
