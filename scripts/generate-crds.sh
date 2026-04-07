#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

CONTROLLER_GEN="${CONTROLLER_GEN:-${ROOT_DIR}/bin/controller-gen}"

"${CONTROLLER_GEN}" rbac:roleName=manager-role crd \
  paths="./..." \
  output:crd:artifacts:config=config/crd/bases \
  output:rbac:artifacts:config=config/rbac

cp "${ROOT_DIR}/config/crd/bases/patchwork.io_patchrules.yaml" \
   "${ROOT_DIR}/charts/patchwork/crds/patchrules.patchwork.io.yaml"

echo "CRDs generated and synced to Helm chart."
