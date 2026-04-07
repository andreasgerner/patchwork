#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

diff "${ROOT_DIR}/config/crd/bases/patchwork.io_patchrules.yaml" \
     "${ROOT_DIR}/charts/patchwork/crds/patchrules.patchwork.io.yaml" || {
  echo "ERROR: Helm chart CRD is out of sync. Run 'make manifests' to fix."
  exit 1
}

echo "CRDs are in sync."
