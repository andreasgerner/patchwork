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
