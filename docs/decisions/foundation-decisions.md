# Foundation decisions

The choices behind the cluster and storage layer as it stands. Each entry says what was decided and why;
the code under `components/` and `scripts/` is the source of truth for details.

## Assets live in `components/<component>/`

Every Helm values file, owned manifest and the version registry sit under `components/`, one directory per
component, created when the component gets its first asset. The `ois` binary will embed this directory, and Go's
`embed` cannot reach outside its module, so assets must be in the module root rather than a sibling `deploy/`.

## One version registry, strictly formatted

All chart versions, app versions and image tags are pinned in `components/versions.env`, one `KEY="value"` per
line with no shell features, because both bash and the Go binary read it. An empty value means "not pinned yet",
never a guess. Image tags are passed to Helm with `--set` from this file, so a chart upgrade cannot silently change
the app version or pull a floating tag. `make check` enforces the format.

## Charts install with `--repo` and `--version`

`helm upgrade --install … --repo <url> --version <pin>`, never `helm repo add`. The user's Helm repository list is
neither read nor modified, and an install does not depend on whatever repos happen to be configured locally.

## Every call names its cluster

Every `kubectl` and `helm` call passes `--context` / `--kube-context` explicitly, and `minikube start` runs with
`--keep-context`. A playground must never touch another cluster by accident, and setting it up must not change the
user's current context.

## Two buckets; tiers are catalog namespaces

`warehouse` holds all Iceberg data and is the catalog's base location; `flink` holds checkpoints and savepoints.
The medallion tiers (`raw`, `staging`, `marts`) are catalog namespaces inside `warehouse`. Iceberg tables carry
their own location, so a bucket per tier would only add credentials and configuration with no isolation gain on a
single node.

## Two Secrets for one credential

`rustfs-root` (`RUSTFS_ACCESS_KEY`, `RUSTFS_SECRET_KEY`) is read by RustFS through `envFrom`, which needs keys that
are valid environment variable names. `ois-storage-credentials` (`access-key`, `secret-key`) is the stable contract
for every S3 client. Both are created from `.env` through a `chmod 600` temp file, never a command-line argument,
and a changed Secret restarts RustFS because `envFrom` is only read at start.

## An owned Service in front of the chart

The stack addresses storage as `rustfs.storage.svc.cluster.local:9000` through a Service it owns, not the chart's
`rustfs-svc`, so a chart upgrade that renames its Service cannot break consumers.

## Memory is sized from measurement

RustFS was OOM-killed at a 256 MiB limit by one 40 MiB upload; three concurrent 64 MiB uploads peak at about
380 MiB. The limit is 768 MiB with a 256 MiB request, and `check-storage.sh` moves a 32 MiB object and fails if
RustFS restarts, so a regression shows up in the check rather than in a Flink job.
