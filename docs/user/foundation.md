# Foundation: cluster and storage

> This is the script-based setup. It will be replaced by the `ois` CLI: once
> `ois up && ois deploy` works, the CLI quickstart takes over from this guide.

This guide brings up a local Minikube cluster with RustFS (S3-compatible storage)
and the `warehouse` and `flink` buckets, checks it from your machine, and tears
it down again. Everything runs from the repository root.

## Prerequisites

| Tool | Version |
| :--- | :--- |
| Docker | Docker Desktop or Engine, running |
| minikube | 1.33 or newer (the cluster runs Kubernetes v1.30) |
| kubectl | within one minor version of v1.30 is best; `cluster-up.sh` warns on a wider skew |
| Helm | 3.12 or newer |
| AWS CLI | v2 (`brew install awscli`) |

Tested with minikube 1.38, kubectl 1.33, Helm 4.3, AWS CLI 2.39 and Docker 29.

Docker must be able to give the cluster enough resources. Two sizing profiles
are defined in [`components/cluster/cluster.env`](../../components/cluster/cluster.env):

| Profile | How | CPU / memory / disk |
| :--- | :--- | :--- |
| Foundation (default) | `./scripts/cluster-up.sh` | 2 vCPU / 4 GiB / 20 GiB |
| Lean full stack | `FULL_STACK=1 ./scripts/cluster-up.sh` | 4 vCPU / 7 GiB / 30 GiB |

The default is enough for storage. See [`components/cluster/README.md`](../../components/cluster/README.md)
for switching profiles.

## Happy path

```bash
[ -f .env ] || cp .env.example .env   # then set RUSTFS_ROOT_USER and RUSTFS_ROOT_PASSWORD (>= 8 chars)
./scripts/cluster-up.sh
./scripts/setup-namespaces.sh
./scripts/deploy-storage.sh
./scripts/check-storage.sh
```

Edit `.env` before `deploy-storage.sh`: the password must be at least 8
characters and not the `change-me…` placeholder. `.env` is gitignored.

The last command should end with:

```text
PASS warehouse
PASS flink
Storage check passed.
```

Every script is safe to re-run.

## What you get

- Minikube profile and kube context `open-iceberg`. The scripts always pass this
  context explicitly and never change your current one, except `cluster-up.sh`,
  which switches to it.
- RustFS in namespace `storage`, with its data on a 10Gi PVC that survives pod restarts.
- In-cluster S3 endpoint `http://rustfs.storage.svc.cluster.local:9000`
  (path-style addressing, region `us-east-1`).
- Buckets `warehouse` (Iceberg data) and `flink` (checkpoints and savepoints).

Open the console at <http://localhost:9001/rustfs/console/> after running:

```bash
kubectl --context open-iceberg -n storage port-forward svc/rustfs 9001:9001
```

Log in with the credentials from `.env`, or read them from the cluster without
writing them to a shared log:

```bash
kubectl --context open-iceberg -n storage get secret ois-storage-credentials -o jsonpath='{.data.access-key}' | base64 -d
kubectl --context open-iceberg -n storage get secret ois-storage-credentials -o jsonpath='{.data.secret-key}' | base64 -d
```

Endpoints, Secrets and buckets are described in [`components/storage/README.md`](../../components/storage/README.md).

## Teardown

```bash
./scripts/cluster-down.sh
```

This deletes the `open-iceberg` profile and **all its data**, including
everything in the buckets. `.env` stays on disk; remove it by hand if you no
longer need it.

## Troubleshooting

| Symptom | Fix |
| :--- | :--- |
| `minikube start` fails or pods are `OOMKilled` / stuck `Pending` for CPU or memory | Give Docker at least the profile's CPU and memory (Docker Desktop → Settings → Resources), then `./scripts/cluster-down.sh && ./scripts/cluster-up.sh`. |
| `cluster '…' is running with … but FULL_STACK=… requests …` | Minikube cannot resize. Re-run with `RECREATE=1`, which deletes the cluster and its data. |
| PVC `rustfs-data` stays `Pending` | The default StorageClass must be `standard`: check `kubectl --context open-iceberg get storageclass` and that the `storage-provisioner` addon is enabled (`minikube -p open-iceberg addons list`). `cluster-up.sh` enables both. |
| `check-storage.sh`: `RustFS not reachable on 127.0.0.1:19000` or port-forward fails | Port 19000 may be taken: `LOCAL_PORT=19010 ./scripts/check-storage.sh`. Otherwise check the pod: `kubectl --context open-iceberg -n storage get pods`. |
| `ERROR: …/.env missing; copy .env.example and set credentials` | Run `cp .env.example .env` and set the two RustFS values. |
| `ERROR: set RUSTFS_ROOT_USER and RUSTFS_ROOT_PASSWORD in .env (…)` | The password is shorter than 8 characters, still the `change-me…` placeholder, or `rustfsadmin`. |
| `ERROR: kube context 'open-iceberg' not found; run scripts/cluster-up.sh first` | The cluster is not up. Run `./scripts/cluster-up.sh`. |
| `check-storage.sh`: `'aws' not found on PATH` | Install AWS CLI v2. |

Do not run the scripts with `bash -x`: tracing prints the credentials.
