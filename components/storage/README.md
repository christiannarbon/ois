# Storage (`components/storage/`)

RustFS is the S3-compatible object store under the lakehouse: Iceberg data and Flink checkpoints both land here.

## What is deployed

`./scripts/deploy-storage.sh` installs, into the `storage` namespace of the `open-iceberg` cluster:

- Helm release `rustfs` (chart pinned in [`versions.env`](../versions.env)), configured by [`values.yaml`](values.yaml): one standalone pod, one replica.
- PVCs `rustfs-data` (10Gi) and `rustfs-logs` (256Mi) on the `standard` StorageClass. Deleting the pod keeps the data; deleting the PVC does not.
- Service `rustfs` from [`service.yaml`](service.yaml). This is the stable name the rest of the stack uses. The chart's own `rustfs-svc` also exists but nothing depends on it.

Running the script again is safe: it upgrades the release in place.

## Endpoints

| What | Address | Notes |
| :--- | :--- | :--- |
| S3 API (in-cluster) | `http://rustfs.storage.svc.cluster.local:9000` | path-style addressing, region `us-east-1` |
| Console (in-cluster) | `http://rustfs.storage.svc.cluster.local:9001` | web UI |

RustFS does not serve virtual-hosted-style requests, so every client must use path-style access (`s3.path-style-access=true`, or `addressing_style = path` for the AWS CLI).

From the host, port-forward both ports:

```bash
kubectl --context open-iceberg -n storage port-forward svc/rustfs 9000:9000 9001:9001
```

The S3 API is then at `http://localhost:9000` and the console at `http://localhost:9001`.

## Credentials

Credentials come from `RUSTFS_ROOT_USER` / `RUSTFS_ROOT_PASSWORD` in the gitignored `.env`. The deploy script turns them into two Secrets in `storage`:

| Secret | Keys | Read by |
| :--- | :--- | :--- |
| `rustfs-root` | `RUSTFS_ACCESS_KEY`, `RUSTFS_SECRET_KEY` | the RustFS chart (`envFrom`) |
| `ois-storage-credentials` | `access-key`, `secret-key` | S3 clients: buckets job, Polaris, Flink, … |

To read a value locally, decode it straight into a variable or your terminal, and never into a shared log:

```bash
kubectl --context open-iceberg -n storage get secret ois-storage-credentials -o jsonpath='{.data.access-key}' | base64 -d
kubectl --context open-iceberg -n storage get secret ois-storage-credentials -o jsonpath='{.data.secret-key}' | base64 -d
```

Never run `deploy-storage.sh` with `bash -x`: tracing prints the `printf` arguments that carry the credentials.
