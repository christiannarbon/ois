# LLD 05 — Distributed Compute Engine

**Layer:** Trino

## Purpose & scope

Deploy Trino as the parallel SQL engine that reads the Iceberg tables Flink
writes — loading manifests from Polaris over OAuth2 and pulling Parquet directly
from RustFS with vended credentials. Trino is a **pure reader** at this layer; it
does not create or write tables. **In scope:** Helm deploy (coordinator +
workers), the Iceberg REST catalog connector, OAuth2 + S3 wiring, CLI/client
access.

## Component internals

```mermaid
flowchart TB
    subgraph trino["Trino (ns: query)"]
        coord[Coordinator :8080<br/>parse · plan · schedule]
        w1[Worker]
        w2[Worker]
        cat[iceberg.properties<br/>connector=iceberg, type=rest]
        coord --- w1
        coord --- w2
        coord -.reads.- cat
    end
    polaris[[Polaris REST :8181]]
    rustfs[(RustFS warehouse)]
    cli[Trino CLI / JDBC / SQLAlchemy]
    cli -->|SQL| coord
    coord -->|OAuth2 + manifests| polaris
    w1 & w2 -->|Parquet via vended creds, path-style| rustfs
```

## Configuration contract — `iceberg.properties`

The catalog connector file is the heart of this layer:

| Property | Value | Notes |
| :--- | :--- | :--- |
| `connector.name` | `iceberg` | — |
| `iceberg.catalog.type` | `rest` | REST catalog, not Hive/Glue |
| `iceberg.rest-catalog.uri` | `http://polaris.catalog.svc.cluster.local:8181/api/catalog` | per the [catalog contract](03-governance-catalog.md) |
| `iceberg.rest-catalog.security` | `OAUTH2` | client-credentials |
| `iceberg.rest-catalog.oauth2.*` | client_id / client_secret / token endpoint | from Secret |
| `iceberg.rest-catalog.vended-credentials-enabled` | `true` | consume Polaris-vended S3 creds |
| `fs.native-s3.enabled` / `s3.*` | RustFS endpoint, **path-style**, region | `s3.path-style-access=true` |

> **Both halves are required.** Pointing the connector at the REST catalog is not
> sufficient on its own: table *metadata* resolves via the REST identity, but
> table *data* only loads once OAuth2 and S3 file access are also wired. A
> connector with only the former will list tables and then fail to read them.

## Deployment shape

| Concern | Setting | Notes |
| :--- | :--- | :--- |
| Deploy | Trino Helm chart, `components/trino/` | pinned version |
| Topology | 1 coordinator + N workers | scale workers for parallelism |
| Endpoint | `trino.query.svc.cluster.local:8080` | in-cluster DNS |
| Catalog mount | `iceberg.properties` as config/ConfigMap | mounted into coordinator + workers |
| Client access | Trino CLI (port-forward), JDBC, SQLAlchemy-Trino | auth posture / `--user` documented |

## Key sequence — a distributed read

```mermaid
sequenceDiagram
    participant U as Trino CLI
    participant C as Coordinator
    participant P as Polaris
    participant W as Workers
    participant M as RustFS
    U->>C: SELECT count(*) FROM iceberg.ns.table
    C->>P: OAuth2 token + resolve snapshot/manifests
    P-->>C: metadata + vended S3 creds
    C->>W: distribute splits (Parquet files)
    W->>M: read Parquet (path-style, temp creds)
    W-->>C: partial counts
    C-->>U: total row count
```

The coordinator resolves metadata once, then fans the Parquet files out as splits
across workers — each worker reads directly from RustFS with the vended
credentials, so data never flows back through Polaris.

## Inputs consumed / outputs produced

**Consumes:** the [catalog contract](03-governance-catalog.md) (REST URI,
OAuth2, vending); the [table contract](04-stream-processing.md) (table id,
schema, partition spec); the [foundation](01-foundation-storage.md) baseline.

**Produces — the Trino connection contract (to orchestration and BI):**

- **Connection contract:** coordinator host/port (in-cluster DNS + local
  port-forward), catalog name `iceberg`, default user/auth posture, and the
  JDBC / SQLAlchemy-Trino connection-string shapes.
- **Read contract:** visible namespaces/tables, vending behavior for a read-only
  engine, session defaults (catalog/schema).
- How Trino is deployed, versioned, and scaled (coordinator + worker count).

Target folders: `components/trino/` (Helm values + `iceberg.properties`),
`scripts/` (validation queries).
