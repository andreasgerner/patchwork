# patchwork

A Kubernetes operator that automatically patches resources based on declarative rules.

Define a `PatchRule` custom resource specifying a target resource type, optional conditions, and the values to set or remove. The operator watches for matching resources and applies a JSON merge patch.

## How it works

Create a `PatchRule`:

```yaml
apiVersion: patchwork.io/v1
kind: PatchRule
metadata:
  name: ingress-aws-lb
spec:
  target:
    apiVersion: networking.k8s.io/v1
    kind: Ingress
    conditions:                        # optional — omit to apply to all
      metadata:
        labels:
          app: sample
  overwrite: true                      # replace existing values (false = only set if absent)
  additions:
    metadata:
      annotations:
        alb.ingress.kubernetes.io/ssl-policy: ELBSecurityPolicy-TLS13-1-2-Res-PQ-2025-09
      labels:
        managed-by: aws-lb-controller
  removals:                             # optional — remove keys from targets
    metadata:
      annotations:
        - deprecated-annotation
```

The `additions`, `removals`, and `conditions` blocks use nested YAML that mirrors the target resource structure. If `conditions` is omitted, all resources of that kind are patched.

**Before:**

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: sample-ingress
  labels:
    app: sample
  annotations:
    alb.ingress.kubernetes.io/ssl-policy: TLS
```

**After:**

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: sample-ingress
  labels:
    app: sample
    managed-by: aws-lb-controller
  annotations:
    alb.ingress.kubernetes.io/ssl-policy: ELBSecurityPolicy-TLS13-1-2-Res-PQ-2025-09
```

## Install

### Helm

```bash
helm install patchwork charts/patchwork \
  --namespace patchwork-system \
  --create-namespace
```

### Helm values

| Key | Default | Description |
|-----|---------|-------------|
| `image.repository` | `ghcr.io/andreasgerner/patchwork` | Container image repository |
| `image.tag` | `appVersion` | Image tag (defaults to chart appVersion) |
| `image.pullPolicy` | `IfNotPresent` | Image pull policy |
| `replicaCount` | `1` | Number of controller replicas |
| `leaderElect` | `true` | Enable leader election for HA |
| `resources.requests.cpu` | `100m` | CPU request |
| `resources.requests.memory` | `64Mi` | Memory request |
| `resources.limits.cpu` | `200m` | CPU limit |
| `resources.limits.memory` | `128Mi` | Memory limit |
| `serviceAccount.create` | `true` | Create a ServiceAccount |
| `serviceAccount.name` | `""` | Override ServiceAccount name |
| `nodeSelector` | `{}` | Node selector |
| `tolerations` | `[]` | Tolerations |
| `affinity` | `{}` | Affinity rules |

### Uninstall

```bash
helm uninstall patchwork -n patchwork-system
```

Note: The `PatchRule` CRD is not removed on uninstall (Helm convention for CRDs). To fully remove:

```bash
kubectl delete crd patchrules.patchwork.io
```

## Build from source

```bash
# Generate deepcopy and CRD manifests
make generate

# Build binary
make build

# Run locally against current kubeconfig
make run

# Build container image
make docker-build IMG=patchwork:dev
```

## Architecture

Built with [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime). The operator:

1. Watches `PatchRule` CRs for changes
2. Dynamically starts watches for each target resource type (Ingress, Deployment, etc.)
3. When a target resource is created or updated, finds matching `PatchRule` CRs and applies a JSON merge patch
4. Skips patching when the target already matches the desired state (no infinite loops)

## Cleanup and revert

The operator tracks every patch it applies per target in the PatchRule's status. A finalizer (`patchwork.io/cleanup`) ensures changes are reverted before the CR is deleted.

- **Delete a PatchRule** -- all additions are reverted and all removed keys are restored on every tracked target.
- **Remove an addition from spec** -- that specific addition is reverted on all targets (restored to its original value, or deleted if it was added by the operator).
- **Remove a removal from spec** -- the previously removed keys are restored with their original values.
- **Target no longer matches conditions** -- all patches on that target are fully reverted.

Prior values are captured before each patch and preserved across reconciles, so reverts always restore the true original state (last-write-wins).

## Conflict detection

Multiple PatchRules can target the same resource kind, but they must not touch the same key paths on the same concrete targets. The operator checks this at reconcile time:

- For each matched target, the rule's addition and removal paths are compared against other rules that already track the same target in their `status.targets`.
- The rule with the **earlier creation timestamp** wins (tie-break: alphabetically lower name).
- The losing rule is rejected: `status.conflicted=true`, `status.active=false`, and a `conflictMessage` naming the winning rule and overlapping paths.
- Rules with **different conditions** that never match the same resources can freely use the same key paths — conflict detection operates on actual matched targets, not abstract condition comparison.
- When the winning rule is deleted or modified to no longer claim the conflicting paths, the conflicted rule is automatically re-evaluated and becomes active.
