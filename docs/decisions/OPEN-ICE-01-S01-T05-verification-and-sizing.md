# OPEN-ICE-01-S01-T05 — Cluster Lifecycle Verification & Resource Sizing Record

- **Status:** APPROVED
- **Date:** 2026-08-15
- **Re-verified:** 2026-09-13 (host container engine migrated from Rancher Desktop to OrbStack)
- **Epic:** OPEN-ICE-01 (Foundation & Storage Substrate)
- **Story:** OPEN-ICE-01-S01 (Provision a local Kubernetes cluster)

## Overview

This document records the empirical lifecycle verification of the local Minikube Kubernetes cluster (`open-iceberg`) and defines the dual resource sizing profiles for `open-iceberg-stack`.

## Validation Host Specifications (Reference Machine)

> **Note:** The hardware specifications below represent the **Validation Host** environment where empirical timing benchmarks were measured. This validation host is **NOT** the minimum system floor required to run the stack.

- **Operating System:** macOS 26.6.2 (Build 25G83, Kernel 25.6.0 arm64)
- **Architecture:** `arm64` (Apple Silicon)
- **CPU Cores:** 14 logical cores
- **System Memory:** 36 GiB RAM (38,654,705,664 bytes)
- **Container Engine:** OrbStack (Docker Engine 29.4.0; VM memory limit 16 GiB). Minikube's `docker` driver runs against it unchanged; minikube labels this "Docker Desktop driver" on macOS regardless of the engine vendor.
- **Validated Tool Versions:**
  - `minikube`: `v1.38.1`
  - `kubectl` client: `v1.33.9` (bundled with OrbStack at `/usr/local/bin/kubectl`)
  - Kubernetes server: `v1.30.0` (pinned via `--kubernetes-version`)
- **Known Warning:** client/server skew is 3 minor versions, so `scripts/cluster-up.sh` and `kubectl version` emit a non-fatal skew warning. No functional impact was observed for the lifecycle checks below.

## Lifecycle Performance Results

The complete create -> ready -> teardown loop was benchmarked on the validation host using the automated repository helper scripts:

| Lifecycle Action | Execution Command | Time Elapsed | Target Budget | Verification Result |
| :--- | :--- | :--- | :--- | :--- |
| **Cluster Bring-Up** | `./scripts/cluster-up.sh` | **26.14 sec** (includes pulling kicbase `v0.0.50`) | < 180 sec (3 min) | PASS (`kubectl get nodes` -> Ready) |
| **StorageClass Check** | `kubectl get storageclass` | **Instant** | Immediate | PASS (`standard (default)` active) |
| **Cluster Teardown** | `./scripts/cluster-down.sh` | **14.39 sec** | < 60 sec | PASS (Profile & context cleanly removed) |

Idle memory of the Foundation profile node container immediately after bring-up: **424.6 MiB / 4 GiB** (`docker stats`).

### Empirical Command Outputs

```text
$ ./scripts/cluster-up.sh
WARNING: kubectl client minor version (33) and target Kubernetes server minor version (30) skew exceeds +/-1 minor version (skew=3).
=======================================================================
Provisioning Minikube Cluster: open-iceberg
Requested Sizing Profile: FULL_STACK=0 (CPUs=2, Memory=4096MB, Disk=20g)
=======================================================================
Starting cluster 'open-iceberg' with parameters: -p open-iceberg --driver=docker --kubernetes-version=v1.30.0 --cpus=2 --memory=4096 --disk-size=20g --interactive=false
* [open-iceberg] minikube v1.38.1 on Darwin 26.6.2 (arm64)
* Using the docker driver based on user configuration
! Starting v1.39.0, minikube will default to "containerd" container runtime. See #21973 for more info.
* Pulling base image v0.0.50 ...
* Enabled addons: storage-provisioner, default-storageclass
* Done! kubectl is now configured to use "open-iceberg" cluster and "default" namespace by default
=======================================================================
Cluster 'open-iceberg' is ready.
Active context: open-iceberg
=======================================================================

$ kubectl get nodes
NAME           STATUS   ROLES           AGE   VERSION
open-iceberg   Ready    control-plane   13s   v1.30.0

$ kubectl get storageclass
NAME                 PROVISIONER                RECLAIMPOLICY   VOLUMEBINDINGMODE   ALLOWVOLUMEEXPANSION   AGE
standard (default)   k8s.io/minikube-hostpath   Delete          Immediate           false                  9s

$ ./scripts/cluster-down.sh
Deleting Minikube profile 'open-iceberg'...
* Removed all traces of the "open-iceberg" cluster.
Cleaning up kubeconfig context for 'open-iceberg'...
Teardown of cluster 'open-iceberg' complete.
```

### Previous Verification (2026-08-15, superseded)

Host engine Rancher Desktop (Docker Engine 29.3.1, reported as `29.1.4-rd`), macOS 26.5.2, `kubectl` client `v1.36.1`. Bring-up **19.05 sec**, teardown **18.71 sec**, `standard (default)` StorageClass present. All checks PASS.

## Resource Sizing Specifications

Two sizing profiles are established for local development:

### 1. Foundation Profile (Default)
- **Allocation:** `--cpus 2 --memory 4096 --disk-size 20g` (2 vCPU / 4 GiB RAM / 20 GiB disk ceiling)
- **Target Workload:** Provisions the local Kubernetes cluster infrastructure and RustFS S3 object storage substrate.
- **Minimum System Requirement Floor:** Dual-core CPU, 8 GiB host RAM. Intentionally lightweight to enable quick local onboarding.

### 2. Lean Full-Stack Profile (Opt-in)
- **Allocation:** `--cpus 4 --memory 7168 --disk-size 30g` (4 vCPU / 7 GiB RAM / 30 GiB disk ceiling)
- **Activation:** `FULL_STACK=1 ./scripts/cluster-up.sh`
- **Target Workload:** Required when deploying the complete analytical data platform (Kafka streaming, Flink processing, Trino query engine, Superset visualization).
- **Resource Budget Policy:** The 30 GiB disk ceiling holds container images alongside lean PVCs (RustFS 10Gi, Kafka 8Gi). Requires strict component resource caps (single replicas, small JVM heap limits).
- **Workload Caveats:** Potential OOM or swap pressure when concurrent data ingestion and heavy analytical queries overlap.

## Quickstart Prerequisites Summary

Contributors running `open-iceberg-stack` must ensure:
1. A Docker-compatible engine active for the Minikube `docker` driver (e.g. OrbStack, Colima, Docker Desktop, Rancher Desktop).
2. Minikube ≥ 1.32 and `kubectl` ≥ 1.27 installed.
3. Minimum 8 GiB host RAM for Foundation profile (16+ GiB recommended for Full-Stack profile).
