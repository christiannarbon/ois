# OPEN-ICE-01-S01-T01 — Default Local Kubernetes Cluster Provider

> **Note:** paths in this record predate the move to `components/`: `deploy/cluster/` is now `components/cluster/`. See [foundation-decisions.md](foundation-decisions.md) for the current choices.

- **Status:** APPROVED
- **Date:** 2026-08-13
- **Epic:** OPEN-ICE-01 (Foundation & Storage Substrate)
- **Story:** OPEN-ICE-01-S01 (Provision a local Kubernetes cluster)

## Decision

**Default Local Kubernetes Provider:** Minikube (profile name: `open-iceberg`).

**Rationale:**
Minikube gives precise CPU, memory, and disk resource allocation control via `--cpus`, `--memory`, and `--disk-size`, which is essential because the downstream analytics stack (Kafka, Flink, Trino, Superset) is heavy and must be sized up front. Its named-profile isolation (`-p open-iceberg`) provides clean context boundaries and disposable teardown. Furthermore, Minikube includes bundled `default-storageclass` and `storage-provisioner` addons out of the box, providing a default `standard` StorageClass without requiring manual dynamic volume provisioner setup in T03. Finally, it features excellent Apple-Silicon/arm64 support via the `docker` driver.

While Minikube is the default target for `deploy/cluster/` and repository helper scripts, alternative local Kubernetes providers (Kind, Docker Desktop Kubernetes) remain allowed as long as the provider interface stays isolated to `deploy/cluster/`.

## Target Tool & Component Versions

- **Minikube:** ≥ 1.32 (Target Kubernetes version: `v1.30.0` via `--kubernetes-version`)
- **kubectl:** ≥ 1.27
- **Client/Server Version Skew:** Target recommendation is within ±1 minor version skew. Client-side forward skew (e.g. `kubectl` client `v1.36.1` against Kubernetes server `v1.30.0`) is accepted and tested, as `kubectl` client maintains backward compatibility with older Kubernetes API server releases for standard resources. `scripts/cluster-up.sh` includes a non-fatal preflight check that issues a warning when skew exceeds 1 minor version.

### Candidate Baseline Requirements
- **Minikube:** ≥ 1.32 (with `docker` driver)
- **Kind:** ≥ 0.20 (node image Kubernetes version ≥ 1.27)
- **Docker Desktop Kubernetes:** Enabled via Docker Desktop settings (subject to enterprise licensing)

## Comparison Matrix

| Evaluation Dimension | Minikube (Default) | Kind | Docker Desktop K8s |
| :--- | :--- | :--- | :--- |
| **Startup Speed** | Moderate (~30-60s) | Fast (~15-30s) | Fast (instant if enabled) |
| **RAM/CPU Overhead** | Medium; precise allocation via `--cpus`/`--memory` | Low to Medium; shares container engine limits | Higher; bundled with Docker Desktop engine |
| **Out-of-box Default StorageClass** | Yes (`standard` via `default-storageclass` addon) | Requires manual default SC configuration or local path provisioner | Yes (`hostpath`) |
| **Apple-Silicon / arm64 Support** | Excellent (via `docker` driver) | Excellent | Excellent |
| **CI Availability** | Good (via Docker in Docker / GitHub Actions) | Excellent (lightweight container execution) | Restricted (requires full desktop app) |
| **Cross-OS Support** | macOS, Linux, Windows | macOS, Linux, Windows | macOS, Windows |
| **Licensing** | Open source (Apache 2.0) | Open source (Apache 2.0) | Commercial license required for enterprise use (>250 employees / >$10M revenue) |

## Swappability & Isolation Guardrails

- `deploy/cluster/` remains the single provider-specific configuration surface.
- Scripts in `scripts/` target Minikube by default, but cluster creation logic must not hard-wire provider assumptions deep into downstream Helm values or application manifests.
- Alternative providers (e.g., Kind, Docker Desktop) must supply a default StorageClass named `standard` (or alias it) if substituted.

## Captured Environment Tool Versions

Captured from local environment:
- **Minikube Version:** `v1.38.1` (commit: `c93a4cb9311efc66b90d33ea03f75f2c4120e9b0`)
- **kubectl Client Version:** `v1.36.1` (Kustomize Version: `v5.8.1`)
- **Kubernetes Server Version:** `v1.30.0`
- **Status:** Installed, captured, and validated in T05/T06.

## Definition of Done

- [x] A short decision record exists naming the default + one-paragraph rationale.
- [x] Validated tool + kubectl versions are recorded (empirically confirmed in T05/T06).
