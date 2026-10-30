# Component assets & deployment baseline (`components/`)

This document defines the deployment tooling baseline, Helm version requirements, installation procedures, namespace conventions, values-file layout, version pinning policy, resource-budget policies, and secrets handling for `open-iceberg-stack`.

## Helm Version Requirement

All chart installations across all epics require **Helm ≥ 3.12**.

- **Version Floor:** Helm 3.12 (or higher) is mandatory for stable OCI (Open Container Initiative) chart registry support, required for charts such as Bitnami OCI releases.
- **Incompatible Versions:** Helm 2 and Tiller are strictly forbidden due to security End-of-Life (EOL) and incompatibility with the stack workflow.
- **Policy:** Do not pin an exact patch version; any build meeting or exceeding `v3.12.0` is supported.

## Installation Instructions

### macOS (Homebrew)

```bash
brew install helm
```

### Linux

Using the official Helm installation script:

```bash
curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
```

Alternatively, use system package managers:
- **Debian / Ubuntu:**
  ```bash
  sudo apt-get install apt-transport-https --yes
  curl https://baltocdn.com/helm/signing.asc | gpg --dearmor | sudo tee /usr/share/keyrings/helm.gpg > /dev/null
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/helm.gpg] https://baltocdn.com/helm/stable/debian/ all main" | sudo tee /etc/apt/sources.list.d/helm-stable-debian.list
  sudo apt-get update
  sudo apt-get install helm
  ```
- **Fedora / RHEL:** `sudo dnf install helm`
- **Snap:** `sudo snap install helm --classic`

### Windows

Using Chocolatey or Winget:

```powershell
# Chocolatey
choco install kubernetes-helm

# Winget
winget install Helm.Helm
```

## Verification

To verify that Helm is installed and meets the minimum version requirement, run:

```bash
helm version --short
```

Expected output should indicate version `v3.12.0` or higher (e.g., `v3.15.2` or `v4.0.5`).

## Values-File Layout Convention

Every stack component maintains its declarative Helm override configuration in a dedicated `values.yaml` file under its respective component directory:

`components/<component>/values.yaml`

Examples:
- Storage Substrate: `components/storage/values.yaml`
- REST Catalog: `components/catalog/values.yaml`
- Streaming Ingestion: `components/ingestion/values.yaml`
- Compute Processing: `components/processing/values.yaml`
- Distributed Query: `components/query/values.yaml`
- Business Intelligence: `components/bi/values.yaml`
- Workflow Orchestration: `components/orchestration/values.yaml`
- Maintenance Utilities: `components/maintenance/values.yaml`

**Convention Rules:**
1. All custom configurations, resource overrides, replica counts, and container arguments **must** be declared within the respective `components/<component>/values.yaml` file.
2. Ad-hoc `--set` flags in deployment scripts are strictly prohibited except for transient dynamic secrets or context parameters.

## Secrets & Credentials Handling Policy

To keep credentials secure during local development and prevent credential leaks in version control:

### 1. Secret Handling Mechanism
- **Secret Values Overlay:** Secret configurations are managed via gitignored secret overlay files (e.g., `components/<component>/values-secret.yaml` or `*.secret.yaml`).
- **Helm Invocation:** Helm installations pass the secret overlay as a second values file flag:
  `helm install <release> <chart> -f components/<component>/values.yaml -f components/<component>/values-secret.yaml`
- **Environment Template:** Environment variables can also be declared in a gitignored `.env` file copied from [`.env.example`](../.env.example).

### 2. Version Control Safety & Guardrails
- **Gitignore Protection:** All secret file patterns (`.env`, `.env.local`, `*-secret.yaml`, `*.secret.yaml`, `*.secret.*`, `values-secret.yaml`) are strictly covered under [`.gitignore`](../.gitignore).
- **Placeholder Templates:** The repository commits **only** non-sensitive placeholder templates (e.g., `.env.example`, `components/storage/values-secret.yaml.example`).
- **No `--set` Leakage:** Secrets **must never** be hardcoded into committed shell script `--set` flags, as shell command lines leak sensitive data into shell history and CI job logs.
- **No Log Echoing:** Installation scripts and CI pipelines **must never** print or echo resolved secret values to stdout or log outputs.

### Verification of Ignore Status
Before committing new files, verify that secret files are ignored by Git:

```bash
git check-ignore .env components/storage/values-secret.yaml test.secret.yaml
```

## Chart-Version Pinning Policy

To guarantee deterministic, reproducible deployments across developer environments and CI pipelines:

1. **Strict Version Pinning:** Every Helm chart version **must** be explicitly pinned to an exact release version. Using `latest`, unpinned charts, or floating version ranges is strictly forbidden.
2. **Single Authoritative Registry:** All pinned chart versions and bundled container `appVersion` targets are recorded in a single authoritative file: [`components/versions.env`](versions.env).
3. **Dual Version Tracking:** For every component, both the **Chart Version** (the packaging metadata version) and the **App Version** (the underlying application container image tag) are explicitly recorded.

### Authoritative Component Version Registry

| Component | Target Namespace | Chart Version | App Version | Pinned Env Variable |
| :--- | :--- | :--- | :--- | :--- |
| **RustFS (Storage)** | `storage` | `0.1.0` | `1.0.0` | `RUSTFS_CHART_VERSION` / `RUSTFS_APP_VERSION` |
| **Iceberg REST Catalog** | `catalog` | `0.1.0` | `0.7.1` | `CATALOG_CHART_VERSION` / `CATALOG_APP_VERSION` |
| **Kafka (Ingestion)** | `ingestion` | `0.41.0` | `3.7.0` | `KAFKA_CHART_VERSION` / `KAFKA_APP_VERSION` |
| **Flink (Processing)** | `processing` | `1.19.0` | `1.19.0` | `FLINK_CHART_VERSION` / `FLINK_APP_VERSION` |
| **Trino (Query Engine)** | `query` | `0.28.0` | `442` | `TRINO_CHART_VERSION` / `TRINO_APP_VERSION` |
| **Superset (BI)** | `bi` | `0.12.8` | `4.0.1` | `SUPERSET_CHART_VERSION` / `SUPERSET_APP_VERSION` |
| **Airflow (Orchestration)** | `orchestration` | `1.13.0` | `2.9.1` | `AIRFLOW_CHART_VERSION` / `AIRFLOW_APP_VERSION` |

## Resource-Budget Policy (Lean ~7 GiB Full Stack)

To ensure the entire lakehouse platform fits within the lean full-stack local profile (~4 vCPU / 7 GiB RAM / 30 GiB disk ceiling), every component's `values.yaml` must strictly adhere to the following resource-budget rules:

### Mandatory Resource Rules:
1. **Single Replica Execution:** All stateful and stateless services (Kafka brokers, Flink TaskManagers, Trino workers, Superset pods, Airflow workers) **must** run with `replicaCount: 1`. Multi-replica or high-availability (HA) topologies are forbidden in local environments.
2. **Explicit Resource Requests & Limits:** Every pod specification **must** declare explicit `resources.requests` and `resources.limits` for CPU and Memory. Uncapped pods are strictly forbidden.
3. **Capped JVM Heaps:** JVM-based components (Kafka, Flink JobManager/TaskManager, Trino coordinator) **must** strictly cap Java heap sizes via environment variables or flags (e.g., `-Xms`, `-Xmx`, `KAFKA_HEAP_OPTS`) to ensure heap allocations do not exceed memory limits and trigger OOM Kills.

### Component Memory Allocation Floor (~7 GiB Total)
- `storage` (RustFS): ~512 MiB RAM
- `catalog` (REST Catalog): ~512 MiB RAM
- `ingestion` (Kafka + Zookeeper/KRaft): ~1.5 GiB RAM (JVM heap capped at 1.0 GiB)
- `processing` (Flink JobManager + TaskManager): ~1.5 GiB RAM (JVM heap capped at 1.0 GiB)
- `query` (Trino Single-Node): ~1.75 GiB RAM (JVM heap capped at 1.25 GiB)
- `bi` (Superset): ~768 MiB RAM
- `orchestration` (Airflow / Maintenance): ~512 MiB RAM

## Namespace Convention & Strategy

`open-iceberg-stack` adopts a **Per-Component Namespace Strategy**.

### Strategy Rationale
- **Isolation & RBAC Scoping:** Scoping architecture layers into dedicated namespaces (`storage`, `catalog`, `ingestion`, `processing`, `query`, `bi`, `orchestration`, `maintenance`) enforces clean ServiceAccount, Secret, and RBAC boundaries between infrastructure layers.
- **Selective Teardown & Lifecycle:** Enables teardown or re-installation of individual components (e.g., resetting `query` or `ingestion`) without disrupting shared storage substrate (`storage`).
- **Resource Quota & Monitoring:** Simplifies resource consumption tracking and memory/CPU limit enforcement per architecture layer.

### Naming Compliance
All namespace names adhere strictly to **DNS-1123 sub-domain specifications**:
- Formatted in lowercase alphanumeric characters or `-`.
- Must start and end with an alphanumeric character.
- Maximum length of 63 characters.

### Defined Per-Component Namespaces
- `storage`: Object storage substrate (RustFS S3).
- `catalog`: Iceberg REST Catalog (Apache Iceberg REST service).
- `ingestion`: Streaming data ingestion substrate (Kafka / Strimzi).
- `processing`: Distributed compute engines (Flink / Spark).
- `query`: Distributed SQL query engine (Trino).
- `bi`: Analytics UI and visualization dashboards (Superset).
- `orchestration`: Workflow and pipeline orchestration (Airflow).
- `maintenance`: System housekeeping and utility jobs.

### Idempotent Creation & Usage
All namespaces are declared in [`components/namespaces.yaml`](namespaces.yaml) and created idempotently via `kubectl apply -f`:

```bash
# Provision or update all per-component namespaces idempotently:
kubectl apply -f components/namespaces.yaml

# Alternatively, run the helper script:
./scripts/setup-namespaces.sh

# Single namespace dry-run apply pattern:
kubectl create namespace storage --dry-run=client -o yaml | kubectl apply -f -
```

*Note:* Bare `kubectl create namespace <ns>` commands are strictly prohibited in scripts as they error if the target namespace already exists. Deploying workloads into the `default` namespace is forbidden.

## Repository Organization (`components/`)

Per-component Helm values and configuration manifests are organized under `components/`. A component directory is created when that component gets its first asset:
- `components/versions.env`: Authoritative chart and appVersion registry.
- `components/namespaces.yaml`: Consolidated per-component Namespace manifest.
- `components/cluster/`: Local Kubernetes cluster definition & provider config.
- `components/storage/`: Object storage substrate (RustFS) Helm values (`components/storage/values.yaml`) and secret overlay template (`components/storage/values-secret.yaml.example`).
- `components/catalog/`: Iceberg REST Catalog values & configuration.
- `components/processing/`: Engine processing component values (Flink / Spark).
- `components/ingestion/`: Streaming ingestion components (Kafka).
- `components/query/`: Query engine components (Trino).
- `components/bi/`: Analytics and visualization components (Superset).
- `components/orchestration/`: Workflow orchestration components (Airflow).
- `components/maintenance/`: Maintenance & housekeeping utility manifests.
