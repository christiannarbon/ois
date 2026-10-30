# Storage (`components/storage/`)

RustFS is the S3-compatible object store under the lakehouse: Iceberg data and Flink checkpoints both land here.

## What is deployed

`./scripts/deploy-storage.sh` installs, into the `storage` namespace of the `open-iceberg` cluster:

- Helm release `rustfs` (chart pinned in [`versions.env`](../versions.env)), configured by [`values.yaml`](values.yaml): one standalone pod, one replica.
- PVCs `rustfs-data` (10Gi) and `rustfs-logs` (256Mi) on the `standard` StorageClass. Deleting the pod keeps the data; deleting the PVC does not.
- Service `rustfs` from [`service.yaml`](service.yaml). This is the stable name the rest of the stack uses. The chart's own `rustfs-svc` also exists but nothing depends on it.

Running the script again is safe: it upgrades the release in place.

## Buckets

| Bucket | Holds |
| :--- | :--- |
| `warehouse` | All Iceberg data; the catalog's base location. Medallion tiers are catalog namespaces inside it, not separate buckets. |
| `flink` | Flink checkpoints (`s3://flink/checkpoints/<stream>`) and savepoints (`s3://flink/savepoints/<stream>`). |

They are created by the Job in [`buckets-job.yaml`](buckets-job.yaml), the only place the bucket list (`BUCKETS`) is defined. The Job runs the pinned AWS CLI image in-cluster against the `rustfs` Service with the `ois-storage-credentials` Secret, and creates a bucket only if `head-bucket` says it is missing, so existing data is never touched.

`deploy-storage.sh` runs the Job at the end of every deploy: it deletes the previous Job (Jobs are immutable), applies the manifest with `__AWSCLI_IMAGE__` replaced by `AWSCLI_IMAGE` from [`versions.env`](../versions.env), and waits for it to complete. If the Job fails, the script prints its logs and exits non-zero. To re-run it, run the deploy script again:

```bash
./scripts/deploy-storage.sh
```

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

## Checking storage

```bash
./scripts/check-storage.sh
```

Needs AWS CLI v2 on the host (`brew install awscli`). The script port-forwards `svc/rustfs` to `127.0.0.1:19000` (override with `LOCAL_PORT=…`). It reads credentials from the `ois-storage-credentials` Secret, so it tests what the cluster actually uses, not `.env`. Then for each of `warehouse` and `flink` it uploads a random object, lists it, downloads it, compares the bytes and deletes it, printing `PASS <bucket>`.

It uses a temporary AWS config with path-style addressing and ignores `~/.aws`, so your own profiles are neither read nor changed. Credentials stay in environment variables and are never printed. Any failure exits non-zero with a `FAIL` or `ERROR` line. On success or failure, it removes its test objects, the port-forward and its temp files.

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
