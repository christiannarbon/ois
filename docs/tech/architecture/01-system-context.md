# 01 — System Context & Component Architecture

How the eight layers connect, and where data and control signals flow between
them. This is the "container diagram" for the whole lakehouse — every box is a
deployable component running in the local Kubernetes cluster (except the human
actors and the source dataset).

Component-internal detail for each layer lives in [`../lld/`](../lld/).

## System context

```mermaid
flowchart TB
    subgraph actors[" "]
        direction LR
        dev([Data / Platform Engineer])
        analyst([BI Consumer / Analyst])
    end

    dataset[(Kaggle eCommerce<br/>clickstream CSV)]

    subgraph k8s["Local Kubernetes cluster (Minikube)"]
        stack[["Open Iceberg Stack"]]
    end

    dataset -->|replayed as JSON events| stack
    dev -->|deploys via Helm,<br/>operates, queries| stack
    stack -->|live dashboards| analyst
```

## Component architecture

Data plane flows **left → right** (ingest → store → serve). The catalog and
orchestrator sit across the middle as the shared control plane.

```mermaid
flowchart LR
    ds[(Kaggle CSV<br/>subset)]

    subgraph ingest["Ingestion"]
        prod[Clickstream Producer<br/>Python Deployment]
        kafka[[Apache Kafka<br/>KRaft · topic: raw-clickstream]]
    end

    subgraph process["Stream Processing"]
        flink[Apache Flink<br/>Kafka→watermark→Iceberg sink]
    end

    subgraph govern["Governance & Catalog"]
        polaris[[Apache Polaris<br/>Iceberg REST Catalog<br/>+ credential vending]]
    end

    subgraph storage["Foundation & Storage"]
        rustfs[(RustFS<br/>S3-compatible<br/>warehouse · flink buckets)]
    end

    subgraph compute["Compute"]
        trino[[Trino<br/>coordinator + workers]]
    end

    subgraph orch["Orchestration, Transform & Maintenance"]
        dagster[Dagster<br/>webserver + daemon + sensor]
        dbt[dbt-trino<br/>silver/gold models]
        maint[Maintenance jobs<br/>OPTIMIZE / expire / orphan]
    end

    subgraph bi["BI & Visualization"]
        superset[[Apache Superset]]
        redis[(Redis cache)]
        pg[(PostgreSQL<br/>metadata)]
    end

    ds --> prod --> kafka --> flink
    flink -->|Parquet data files| rustfs
    flink -->|commit snapshot| polaris

    trino -->|load manifests / OAuth2| polaris
    trino -->|read Parquet / vended creds| rustfs

    dagster -->|orchestrates| dbt
    dbt -->|runs SQL on| trino
    dagster -->|schedules| maint
    maint -->|maintenance SQL| trino
    dagster -.->|sensor watches commits| polaris

    superset -->|SQLAlchemy-Trino| trino
    superset --- redis
    superset --- pg
```

### Reading the diagram

- **The only writer of raw data is Flink.** It writes Parquet to RustFS and
  commits the metadata swap to Polaris on every successful checkpoint. Nothing
  else writes the raw table.
- **Trino is the universal read/execute engine.** dbt (transforms), Superset
  (BI), and the maintenance jobs *all* run their SQL through Trino — none of them
  talk to RustFS or Polaris directly for table data.
- **Polaris is the single source of truth for table state.** Every engine
  resolves "what is the current snapshot / where are the files" through the REST
  catalog, and receives scoped RustFS credentials from it (see
  [04 — Governance & Security](04-governance-and-security.md)).
- **Dagster is the control plane for transforms and maintenance.** Its sensor
  keys off Iceberg commits (the checkpoint cadence from Flink) to trigger dbt;
  its schedules drive the day-two maintenance SQL.

## Control-plane vs data-plane responsibilities

| Component | Data plane | Control plane |
| :--- | :--- | :--- |
| Kafka | carries raw events | — |
| Flink | writes Parquet + Iceberg data files | commits snapshots to Polaris |
| RustFS | stores all Parquet + metadata objects | — |
| Polaris | — | catalog state, RBAC, credential vending |
| Trino | reads/writes table data on behalf of engines | — |
| Dagster | — | sensor triggers + maintenance schedules |
| dbt | issues transform SQL (via Trino) | model dependency graph |
| Superset | reads gold tables (via Trino) | dashboard/dataset definitions |

## Layer contracts

Each layer depends on a written contract published by the layers beneath it.
These are the real "interfaces" of the architecture — each one is specified in
full in the corresponding [low-level design](../lld/):

```mermaid
flowchart LR
    L1[Foundation<br/>& Storage] -->|cluster · StorageClass · RustFS endpoint · buckets| L2[Ingestion]
    L1 --> L3[Governance<br/>& Catalog]
    L3 -->|REST URI · OAuth2 client · vending mode| L4[Stream<br/>Processing]
    L2 -->|topic · JSON schema · bootstrap DNS| L4
    L4 -->|table id · schema · partition spec · commit cadence| L5[Compute<br/>Trino]
    L5 -->|Trino connection contract| L6[Orchestration<br/>& Transform]
    L4 -->|small-file behavior| L7[Day-Two<br/>Operations]
    L6 -->|gold model/metric contract| L8[BI &<br/>Visualization]
    L6 --> L7
    L5 --> L7
    L5 --> L8
    L7 -->|perf guarantees| L8
```

Read an edge as "publishes to": the foundation layer publishes a cluster and
bucket set that ingestion consumes; the catalog publishes a REST URI and vending
mode that stream processing consumes, and so on. A layer may only depend on
contracts, never on another layer's internals.
