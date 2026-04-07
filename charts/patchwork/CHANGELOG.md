# Changelog

All notable changes to the patchwork Helm chart will be documented in this file.

## [0.1.0] - 2024-12-01

### Added
- Initial release of the patchwork Helm chart.
- PatchRule CRD for declarative resource patching.
- Conflict detection with priority-based resolution.
- Automatic revert on PatchRule deletion or spec change.
- Optional Prometheus ServiceMonitor.
- PodDisruptionBudget for HA deployments.
