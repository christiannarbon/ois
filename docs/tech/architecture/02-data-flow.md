# 02 — End-to-End Data Flow

How a single clickstream event travels from a CSV row to a pixel on a dashboard,
and how the data is progressively refined across the medallion tiers.

## The medallion tiers

Data is refined in stages. All Iceberg tables live in the `warehouse` bucket; tiers are catalog namespaces.

| Tier | Catalog namespace | Owner | Contents |
| :--- | :--- | :--- | :--- |
| **Raw** | `raw` | Flink | Append-only clickstream events, as ingested |
| **Silver** | `staging` | dbt staging | Cleansed / typed / deduplicated events |
| **Gold** | `marts` | dbt marts | Business aggregates (funnels, cart-abandonment, trending products) |

## End-to-end flow

```mermaid
flowchart TB
    csv[(Kaggle CSV row)]
    prod[Producer<br/>CSV → JSON]
    topic[[Kafka topic<br/>raw-clickstream]]

    csv --> prod -->|continuous JSON events| topic

    subgraph flinkbox["Flink stateful job"]
        parse[Parse JSON<br/>+ assign event-time]
        wm[Apply watermarks<br/>bounded out-of-order]
        sink[Iceberg sink]
        parse --> wm --> sink
    end
    topic --> parse

    subgraph rawtier["RAW tier"]
        rawtbl[(Iceberg raw table<br/>ns: raw)]
    end
    sink -->|Parquet + snapshot per checkpoint| rawtbl

    subgraph transform["dbt via Trino"]
        stg[Staging models<br/>clean · type · dedup]
        mart[Mart models<br/>aggregate · dimensional]
        stg --> mart
    end
    rawtbl -->|read via Trino| stg

    subgraph silvertier["SILVER tier"]
        silvertbl[(Iceberg silver tables<br/>ns: staging)]
    end
    subgraph goldtier["GOLD tier"]
        goldtbl[(Iceberg gold tables<br/>ns: marts)]
    end
    stg --> silvertbl --> mart --> goldtbl

    subgraph serve["Superset"]
        dash[Dashboards<br/>funnel · trending · abandonment]
    end
    goldtbl -->|read via Trino + Redis cache| dash
```

## The self-driving trigger loop

The pipeline advances **without a scheduler polling on a fixed clock** — it is
event-driven off Iceberg commits:

```mermaid
sequenceDiagram
    participant F as Flink
    participant P as Polaris (catalog)
    participant S as Dagster sensor
    participant D as dbt (via Trino)
    participant G as Gold tables

    loop every checkpoint (~1 min)
        F->>P: commit new snapshot (raw table)
    end
    S->>P: poll latest snapshot id
    Note over S: new commit detected?
    S->>D: trigger materialization run
    D->>G: rebuild silver → gold models
    Note over G: fresh aggregates land
```

## Maintenance overlay

Streaming writes create thousands of tiny Parquet files and an ever-growing
snapshot history. Scheduled Dagster jobs run maintenance SQL through Trino to
keep the tables performant — critically, **safe against Flink writing
concurrently**:

```mermaid
flowchart LR
    sched[Dagster schedule<br/>overnight] --> opt[OPTIMIZE<br/>compact small files]
    opt --> exp[expire_snapshots<br/>drop old history]
    exp --> orph[remove_orphan_files<br/>reclaim storage]
    orph --> rustfs[(RustFS:<br/>~1GB files, less history)]
```

- **Order matters:** compact → expire snapshots → remove orphans.
- **Retention must exceed the in-flight write window** so a concurrent Flink
  commit is never treated as orphaned.
- Compaction targets **closed partitions**, never the hot one Flink is writing.

See [LLD 07](../lld/07-day-two-operations.md) for the safety model and SQL.

## Freshness vs. performance

The dashboard is both **live** (new events appear within the refresh window) and
**sub-second** (cached). Redis caching sits between Superset and Trino; the
auto-refresh window is tuned so cache TTL is short enough to reflect new gold
data but long enough to keep repeated loads fast.

See [LLD 08](../lld/08-bi-visualization.md) for how the two are reconciled.
