# Component assets & deployment baseline (`components/`)

This document defines the deployment tooling baseline, Helm version requirements, installation procedures, namespace conventions, values-file layout, version pinning policy, resource-budget policies, and secrets handling for `open-iceberg-stack`.

## Helm Version Requirement

All chart installations across all epics require **Helm ≥ 3.12**.

- **Version Floor:** Helm 3.12 (or higher) is mandatory for stable OCI (Open Container Initiative) chart registry support, required for charts published as `oci://` artifacts.
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
- Storage: `components/storage/values.yaml`
- Ingestion: `components/kafka/values.yaml`
- Catalog: `components/catalog/values.yaml`
- Processing: `components/flink/values.yaml`
- Query: `components/trino/values.yaml`
- Orchestration: `components/dagster/values.yaml`
- BI: `components/superset/values.yaml`

**Convention Rules:**
1. All custom configurations, resource overrides, replica counts, and container arguments **must** be declared within the respective `components/<component>/values.yaml` file.
2. Ad-hoc `--set` flags in deployment scripts are strictly prohibited except for transient dynamic secrets or context parameters.

## Secrets & Credentials Handling Policy

To keep credentials secure during local development and prevent credential leaks in version control:

### 1. Secret Handling Mechanism
- **Source:** Credentials live in a gitignored `.env` file copied from [`.env.example`](../.env.example).
- **Kubernetes Secrets:** Deploy scripts read only the keys they need from `.env` and turn them into Secrets. Values go through a `chmod 600` temp file passed to `kubectl create secret generic --from-env-file`, piped as `--dry-run=client -o yaml` into `kubectl apply -f -`, so re-runs are idempotent. The temp file is removed on exit.
- **Charts read Secrets, not values:** Helm values files never hold credentials; charts point at an existing Secret (e.g. `secret.existingSecret`).
- **Storage example:** `./scripts/deploy-storage.sh` creates two Secrets in `storage` from `RUSTFS_ROOT_USER` / `RUSTFS_ROOT_PASSWORD`:

  | Secret | Keys | Read by |
  | :--- | :--- | :--- |
  | `rustfs-root` | `RUSTFS_ACCESS_KEY`, `RUSTFS_SECRET_KEY` | the RustFS chart (`envFrom`) |
  | `ois-storage-credentials` | `access-key`, `secret-key` | buckets job, Polaris, Flink and other S3 clients |
- **Never trace deploy scripts:** running them with `bash -x` prints the `printf` arguments that carry the credentials. Do not trace them in shared logs.

### 2. Version Control Safety & Guardrails
- **Gitignore Protection:** All secret file patterns (`.env`, `.env.local`, `*-secret.yaml`, `*.secret.yaml`, `*.secret.*`, `values-secret.yaml`) are strictly covered under [`.gitignore`](../.gitignore).
- **Placeholder Templates:** The repository commits **only** non-sensitive placeholder templates (e.g., `.env.example`).
- **No `--set` Leakage:** Secrets **must never** be hardcoded into committed shell script `--set` flags, as shell command lines leak sensitive data into shell history and CI job logs.
- **No Log Echoing:** Installation scripts and CI pipelines **must never** print or echo resolved secret values to stdout or log outputs.

### Verification of Ignore Status
Before committing new files, verify that secret files are ignored by Git:

```bash
git check-ignore .env test.secret.yaml
```

## Chart-Version Pinning Policy

To guarantee deterministic, reproducible deployments across developer environments and CI pipelines:

1. **Strict Version Pinning:** Every Helm chart version **must** be explicitly pinned to an exact release version. Using `latest`, unpinned charts, or floating version ranges is strictly forbidden.
2. **Single Authoritative Registry:** All pinned chart versions and bundled container `appVersion` targets are recorded in a single authoritative file: [`components/versions.env`](versions.env).
3. **Dual Version Tracking:** For every component, both the **Chart Version** (the packaging metadata version) and the **App Version** (the underlying application container image tag) are explicitly recorded.

### Version Registry Format

Pins live only in [`components/versions.env`](versions.env). The `ois` binary parses it, so the format is strict:
- one `KEY="value"` per line; `#` comments and blank lines are allowed, nothing else (no `export`, no `${…}` expansion, no command substitution);
- keys are `UPPER_SNAKE_CASE`;
- an empty value means the component is not pinned yet. It gets pinned when that component is added, never guessed.

## Installing Charts

Charts are installed straight from their repository URL with a pinned version:

```bash
helm upgrade --install <release> <chart> --repo "$<X>_CHART_REPO" --version "$<X>_CHART_VERSION"
```

or from an `oci://` reference with `--version`. `helm repo add` is never used: the repository list on the user's machine is never read or modified.

## Resource-Budget Policy (Lean ~7 GiB Full Stack)

To ensure the entire lakehouse platform fits within the lean full-stack local profile (~4 vCPU / 7 GiB RAM / 30 GiB disk ceiling), every component's `values.yaml` must strictly adhere to the following resource-budget rules:

### Mandatory Resource Rules:
1. **Single Replica Execution:** All stateful and stateless services (Kafka brokers, Flink TaskManagers, Trino workers, Superset pods, Dagster pods) **must** run with `replicaCount: 1`. Multi-replica or high-availability (HA) topologies are forbidden in local environments.
2. **Explicit Resource Requests & Limits:** Every pod specification **must** declare explicit `resources.requests` and `resources.limits` for CPU and Memory. Uncapped pods are strictly forbidden.
3. **Capped JVM Heaps:** JVM-based components (Kafka, Flink JobManager/TaskManager, Trino coordinator) **must** strictly cap Java heap sizes via environment variables or flags (e.g., `-Xms`, `-Xmx`, `KAFKA_HEAP_OPTS`) to ensure heap allocations do not exceed memory limits and trigger OOM Kills.

### Component Memory Allocation Floor (~7 GiB Total)
- `storage` (RustFS): ~512 MiB RAM
- `catalog` (REST Catalog): ~512 MiB RAM
- `ingestion` (Kafka, KRaft): ~1.5 GiB RAM (JVM heap capped at 1.0 GiB)
- `processing` (Flink JobManager + TaskManager): ~1.5 GiB RAM (JVM heap capped at 1.0 GiB)
- `query` (Trino Single-Node): ~1.75 GiB RAM (JVM heap capped at 1.25 GiB)
- `bi` (Superset): ~768 MiB RAM
- `orchestration` (Dagster, incl. maintenance jobs): ~512 MiB RAM

## Namespace Convention & Strategy

`open-iceberg-stack` adopts a **Per-Component Namespace Strategy**.

### Strategy Rationale
- **Isolation & RBAC Scoping:** Scoping architecture layers into dedicated namespaces (`storage`, `ingestion`, `catalog`, `processing`, `query`, `orchestration`, `bi`) enforces clean ServiceAccount, Secret, and RBAC boundaries between infrastructure layers.
- **Selective Teardown & Lifecycle:** Enables teardown or re-installation of individual components (e.g., resetting `query` or `ingestion`) without disrupting shared storage substrate (`storage`).
- **Resource Quota & Monitoring:** Simplifies resource consumption tracking and memory/CPU limit enforcement per architecture layer.

### Naming Compliance
All namespace names adhere strictly to **DNS-1123 sub-domain specifications**:
- Formatted in lowercase alphanumeric characters or `-`.
- Must start and end with an alphanumeric character.
- Maximum length of 63 characters.

### Defined Per-Component Namespaces
- `storage`: Object storage substrate (RustFS S3).
- `ingestion`: Streaming data ingestion (Kafka via Strimzi).
- `catalog`: Iceberg REST catalog (Apache Polaris).
- `processing`: Stream processing engine (Flink).
- `query`: Distributed SQL query engine (Trino).
- `orchestration`: Workflow orchestration and table maintenance jobs (Dagster).
- `bi`: Analytics UI and visualization dashboards (Superset).

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

Component assets live under `components/`, one directory per component. Today it holds:
- `components/versions.env`: Pinned chart, image and jar versions.
- `components/namespaces.yaml`: Per-component Namespace manifest.
- `components/cluster/`: Local Kubernetes cluster definition & provider config.
- `components/storage/`: Object storage (RustFS) Helm values and secret overlay template.

Later components add their own directory when they get their first asset: `kafka/`, `catalog/`, `flink/`, `trino/`, `dagster/`, `superset/`. Images the stack builds itself live in `images/` (`flink/`, `dagster/`), and the clickstream sample lives in `samples/clickstream/` (`producer/`, `transform/`, `dashboards/`).
