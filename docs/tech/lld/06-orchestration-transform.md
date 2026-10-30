# LLD 06 — Asset-Driven Orchestration & Transformation

**Layer:** Dagster + dbt (`dbt-trino`)

## Purpose & scope

Deploy Dagster as orchestrator and dbt (`dbt-trino`) as the transform layer,
turning the raw streaming table into clean **silver** and aggregate **gold**
models — and make them **auto-materialize** when new data lands via an
event-driven sensor watching Iceberg commits. Unlike the read-only compute layer,
dbt **writes** tables, so its principal needs create/write grants. **In scope:**
Dagster deploy, dbt project + adapter, staging + mart models,
dbt-as-Dagster-assets, the sensor.

## Component internals

```mermaid
flowchart TB
    subgraph dagster["Dagster (ns: orchestration)"]
        web[Webserver :3000]
        daemon[Daemon<br/>schedules + sensors]
        code[Code location<br/>Definitions]
        sensor[Iceberg-commit sensor]
        assets[dbt models as<br/>software-defined assets]
        web --- daemon --- code
        code --- sensor
        code --- assets
    end
    subgraph dbt["dbt project (samples/clickstream/transform)"]
        stg[staging models → silver]
        mart[mart models → gold]
        stg --> mart
    end
    trino[[Trino :8080]]
    polaris[[Polaris REST]]
    assets --> dbt
    dbt -->|SQL| trino
    sensor -.->|poll latest snapshot| polaris
    trino -->|write silver/gold tables| polaris
```

## The transform graph (medallion)

```mermaid
flowchart LR
    raw[(raw table<br/>Flink)] --> stg[stg_* models<br/>clean · type · dedup]
    stg --> silver[(silver tables)]
    silver --> funnel[mart: checkout funnel]
    silver --> aband[mart: hourly cart-abandonment]
    silver --> trend[mart: trending products]
    funnel & aband & trend --> gold[(gold tables)]
```

## Configuration contract

| Concern | Setting | Notes |
| :--- | :--- | :--- |
| Dagster deploy | Helm, `components/dagster/` | webserver + daemon + run/event storage |
| dbt adapter | `dbt-trino` | profile → [Trino connection contract](05-compute-engine.md) |
| dbt target | catalog `iceberg`, silver/gold schemas | writes to catalog namespaces `staging`/`marts` in the `warehouse` bucket |
| Write grants | dbt principal: create/write on silver+gold ns | from the [catalog RBAC model](03-governance-catalog.md) |
| Materialization | staging (table/incremental), marts (table/incremental) | incremental adds snapshots → [maintenance surface](07-day-two-operations.md) |
| Assets | dbt models loaded as SDAs | one Dagster asset per dbt model |
| Sensor | watches Iceberg **commit signal** on raw table | keys off Flink's per-checkpoint snapshot |

## Key sequence — event-driven auto-materialization

```mermaid
sequenceDiagram
    participant F as Flink
    participant P as Polaris
    participant S as Dagster sensor
    participant D as dbt (via Trino)
    participant G as Gold tables
    F->>P: commit snapshot (raw)
    loop sensor tick
        S->>P: latest raw snapshot id
        Note over S: changed since last run?
        S->>D: RunRequest → materialize assets
        D->>G: rebuild staging → marts
    end
    Note over G: fresh gold aggregates, no human "run"
```

The sensor keys off the snapshot id rather than a clock, so transforms run
exactly as often as data actually lands — no polling schedule to tune, and no
empty runs between checkpoints.

## Inputs consumed / outputs produced

**Consumes:** the [Trino connection contract](05-compute-engine.md); the
[table contract](04-stream-processing.md) + commit cadence (the sensor signal);
[write grants](03-governance-catalog.md); the
[foundation](01-foundation-storage.md) baseline + buckets.

**Produces — model/orchestration contracts (to maintenance and BI):**

- **Model/table contract:** the gold (and silver) tables — identifiers, grains,
  key columns, materialization strategy, refresh semantics.
- **New compaction surface:** additional Iceberg tables + incremental snapshots —
  input to the [maintenance scope](07-day-two-operations.md) alongside Flink's
  raw table.
- **Orchestration contract:** which assets/jobs exist, what the sensor watches
  and triggers, how to run/observe a materialization.

Target folders: `components/dagster/` (Helm values), `images/dagster/` (code
location, sensor, asset definitions), `samples/clickstream/transform/` (dbt project, models,
`profiles.yml`), `scripts/`.
