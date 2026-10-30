# LLD 04 — Stateful Stream Processing & Iceberg Integration

**Layer:** Apache Flink → Iceberg

## Purpose & scope

Deploy Flink to consume `raw-clickstream`, apply event-time + watermarks, and
write Parquet to RustFS — committing one Iceberg snapshot through Polaris on every
successful checkpoint. Flink is the **first and only writer** of the raw table.
**In scope:** Flink Operator, a custom image (Iceberg + REST + S3 + Kafka jars),
catalog/table wiring, the job, checkpointing, deploy.

## Component internals

```mermaid
flowchart TB
    subgraph op["Flink K8s Operator (ns: processing)"]
        operator[Operator<br/>reconciles FlinkDeployment CR]
    end
    subgraph fd["FlinkDeployment (custom image)"]
        jm[JobManager]
        tm[TaskManagers]
        subgraph job["Job pipeline"]
            src[Kafka source<br/>raw-clickstream]
            deser[JSON deserialize<br/>+ assign event-time]
            water[Watermark strategy<br/>bounded out-of-order]
            isink[Iceberg sink<br/>REST catalog]
            src --> deser --> water --> isink
        end
        jm --- tm
    end
    operator -->|manages| fd
    kafka[[Kafka]] --> src
    polaris[[Polaris REST]] <-->|catalog + OAuth2 + vending| isink
    rustfs[(RustFS warehouse + flink)]
    isink -->|Parquet data files| rustfs
    isink -->|checkpoint state| rustfs
```

## The custom image

Vanilla Flink cannot talk to an Iceberg REST catalog on RustFS out of the box —
the required connectors are not on its classpath. A custom image bakes them in:

```mermaid
flowchart LR
    base[flink:base] --> ib[iceberg-flink-runtime]
    base --> rest[iceberg REST catalog client]
    base --> s3[hadoop-aws / s3 filesystem]
    base --> kc[flink-kafka connector]
    ib & rest & s3 & kc --> img[custom flink image<br/>versioned + loaded into cluster]
```

## Configuration contract

| Concern | Setting | Notes |
| :--- | :--- | :--- |
| Operator | Flink K8s Operator, `components/flink/` | manages `FlinkDeployment` CRs |
| Image | custom `Dockerfile` in `images/flink/` | Iceberg + REST + S3 + Kafka jars, pinned |
| Catalog | type `rest`, URI → Polaris, OAuth2 client-credentials | vending mode per the [catalog contract](03-governance-catalog.md) |
| S3 filesystem | endpoint → RustFS, **path-style**, vended creds | `s3.path-style-access=true` |
| Source | Kafka `raw-clickstream`, JSON format | schema per the [ingestion contract](02-ingestion-backbone.md) |
| Time semantics | event-time from `event_time`, watermarks | bounded out-of-orderness |
| Checkpointing | interval ~1 min, state backend → RustFS/S3 | **checkpoint = 1 Iceberg snapshot** |
| Table | `iceberg.<ns>.<table>` + schema + partition spec | defined via DDL |
| Write props | Iceberg format version, target file size, distribution mode | drives the [small-file surface](07-day-two-operations.md) |

## Key sequence — checkpoint drives snapshot commit

```mermaid
sequenceDiagram
    participant K as Kafka
    participant F as Flink job
    participant M as RustFS
    participant P as Polaris
    K->>F: clickstream events
    F->>F: watermark + buffer
    Note over F: checkpoint barrier (~1 min)
    F->>M: flush Parquet data files
    F->>P: commit new snapshot (metadata swap)
    P-->>F: commit ok
    Note over M,P: Parquet in RustFS + new snapshot in catalog
```

## Inputs consumed / outputs produced

**Consumes:** the [catalog contract](03-governance-catalog.md) (REST URI,
OAuth2 client, vending mode); the [ingestion contract](02-ingestion-backbone.md)
(topic, JSON schema, bootstrap DNS); the
[foundation](01-foundation-storage.md) (cluster, StorageClass, Helm).

**Produces — the table contract (to compute, orchestration and maintenance):**

- **Table identifier** `<catalog>.<namespace>.<table>`, schema, partition spec —
  the stable shape Trino and dbt read.
- **Commit cadence** (one snapshot per checkpoint) and the resulting **small-file
  behavior** — the direct input to [day-two operations](07-day-two-operations.md).
- **Write properties** (format version, target file size, distribution mode) and
  delivery guarantees.
- How the custom image is built, versioned, and loaded.

Target folders: `components/flink/` (operator + `FlinkDeployment`),
`images/flink/` (job source + custom image), `scripts/`.
