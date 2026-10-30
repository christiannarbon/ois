# LLD 07 — Day-Two Operations & Automated Maintenance

**Layer:** Trino SQL + scheduled Dagster jobs

## Purpose & scope

Fix the **small-file problem**: Flink commits a snapshot per checkpoint (~1/min)
and dbt incremental models append per sensor run — together producing thousands
of tiny Parquet files and an ever-growing snapshot history. Trino maintenance SQL
wrapped in **scheduled Dagster jobs** lets the lakehouse maintain itself
overnight — safely, while **Flink keeps writing**. **In scope:**
baseline/measurement, compaction SQL, expiration + orphan-removal SQL, Dagster
scheduling.

## The problem being solved

```mermaid
flowchart LR
    subgraph before["Before maintenance"]
        f1[(tiny.parquet)]
        f2[(tiny.parquet)]
        f3[(tiny.parquet)]
        fn[(… thousands)]
        snaps[long snapshot history]
    end
    subgraph after["After overnight run"]
        big[(~1GB.parquet)]
        few[trimmed history]
        clean[orphans reclaimed]
    end
    before -->|OPTIMIZE → expire → orphans| after
```

## Maintenance operations & order

```mermaid
flowchart LR
    s0[Baseline<br/>file count/size/dist] --> s1
    subgraph seq["Ordered maintenance (per table)"]
        s1["1 · OPTIMIZE<br/>ALTER TABLE ... EXECUTE OPTIMIZE<br/>compact small files"]
        s2["2 · expire_snapshots<br/>CALL system.expire_snapshots<br/>drop old history"]
        s3["3 · remove_orphan_files<br/>CALL system.remove_orphan_files<br/>reclaim storage"]
        s1 --> s2 --> s3
    end
```

**Why this order:** compaction *creates* new files and orphans old ones →
expiring snapshots releases references to superseded files → orphan removal then
physically reclaims anything no longer referenced. Running orphan removal first
would miss what compaction is about to orphan.

## Safety model — maintenance vs. a live writer

This is the central, non-obvious constraint of this layer: **Flink is writing the
whole time.** Maintenance never gets a quiet window to work in, so every
operation must be safe against concurrent commits.

```mermaid
flowchart TB
    subgraph rules["Safety rules"]
        r1[Retention windows must EXCEED<br/>the in-flight write window]
        r2[Compaction targets CLOSED partitions,<br/>never the hot one Flink writes]
        r3[expire_snapshots older_than > max checkpoint lag]
        r4[remove_orphan_files older_than > longest write]
    end
    flink[Flink live commits] -. must never be treated as orphaned .-> r1
    flink -. must not collide with .-> r2
```

## Configuration contract

| Concern | Setting | Notes |
| :--- | :--- | :--- |
| SQL location | the Dagster code location in `images/dagster/` | compaction / expiration / orphan SQL |
| Execution engine | **[Trino](05-compute-engine.md)** | `ALTER TABLE ... EXECUTE OPTIMIZE`, `CALL system.*` |
| Scheduler | **[Dagster](06-orchestration-transform.md)** jobs/schedules | siblings/extension of the code location |
| Target file size | ~1GB (per [Flink write props](04-stream-processing.md)) | compaction target |
| Snapshot retention | `older_than` > max in-flight write window | safety constraint |
| Orphan retention | `older_than` > longest write | safety constraint |
| Scope | Flink raw table + dbt incremental silver/gold | whole maintained table set |
| Grants | maintenance principal: write + metadata ops | commits through Polaris |

## Key sequence — overnight consolidation

```mermaid
sequenceDiagram
    participant Sch as Dagster schedule
    participant T as Trino
    participant P as Polaris
    participant M as RustFS
    participant F as Flink (concurrent)
    Note over F: still writing hot partition
    Sch->>T: OPTIMIZE (closed partitions)
    T->>M: rewrite tiny files → ~1GB
    T->>P: commit compaction
    Sch->>T: expire_snapshots (older_than retention)
    T->>P: drop old snapshots
    Sch->>T: remove_orphan_files (older_than retention)
    T->>M: delete unreferenced files
    Note over M: consolidated, live data intact
```

## Inputs consumed / outputs produced

**Consumes:** the [commit cadence + live-writer constraint](04-stream-processing.md);
[Trino](05-compute-engine.md) as executor; the
[Dagster scheduler + dbt tables](06-orchestration-transform.md);
[catalog grants](03-governance-catalog.md); [RustFS](01-foundation-storage.md).

**Produces — the maintenance contract (to BI and operators):**

- **Schedule & cadence** per table (when OPTIMIZE / expire / orphan-removal run),
  target file size, retention windows.
- **Safety rules** for running against the live Flink writer.
- The **operation order** (compact → expire → orphans) and why.

Target folders: the Dagster code location in `images/dagster/` (the maintenance
SQL and the jobs + schedules that execute it, alongside the transform code;
maintenance is configured by the spec), `scripts/`
(measurement).
