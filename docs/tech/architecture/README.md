# Architecture — Open Iceberg Stack

> **Blueprint A — Governed Enterprise Lakehouse (Local Kubernetes).**
> A fully containerized, governed, streaming data lakehouse built from
> open-source components on a local Kubernetes cluster, replaying the
> [eCommerce Behavior](https://www.kaggle.com/datasets/mkechinov/ecommerce-behavior-data-from-multi-category-store)
> clickstream dataset from raw events to a live BI dashboard.

These documents are the **high-level (C4 context/container-level) view** of the
system. For component-internal detail (config contracts, sequence flows,
per-engine wiring) see the low-level designs in [`../lld/`](../lld/).

## Diagram index

| # | Document | What it answers |
| :- | :--- | :--- |
| 01 | [System Context & Components](01-system-context.md) | What are the moving parts and how do they connect? |
| 02 | [End-to-End Data Flow](02-data-flow.md) | How does a clickstream event travel from Kafka to a dashboard, across the medallion tiers? |
| 03 | [Kubernetes Deployment Topology](03-deployment-topology.md) | What is deployed where — Helm releases, services, PVCs, in-cluster DNS? |
| 04 | [Governance & Security](04-governance-and-security.md) | How does Polaris govern the catalog and vend scoped S3 credentials over OAuth2? |

## The stack at a glance

| Layer | Technology | Role | Design |
| :--- | :--- | :--- | :--- |
| Foundation & storage | Minikube, RustFS, Helm | K8s substrate + S3-compatible object store | [LLD 01](../lld/01-foundation-storage.md) |
| Ingestion | Kafka (KRaft), Python producer | Streaming message backbone | [LLD 02](../lld/02-ingestion-backbone.md) |
| Governance & catalog | Apache Polaris | Iceberg REST catalog + credential vending | [LLD 03](../lld/03-governance-catalog.md) |
| Stream processing | Apache Flink, Apache Iceberg | Kafka → watermarked → Iceberg writer | [LLD 04](../lld/04-stream-processing.md) |
| Compute engine | Trino | Distributed SQL read engine | [LLD 05](../lld/05-compute-engine.md) |
| Orchestration & transform | Dagster, dbt (`dbt-trino`) | Event-driven medallion transforms | [LLD 06](../lld/06-orchestration-transform.md) |
| Day-two operations | Trino SQL, Dagster | Compaction + snapshot/orphan maintenance | [LLD 07](../lld/07-day-two-operations.md) |
| BI & visualization | Superset, Redis, PostgreSQL | Live dashboards | [LLD 08](../lld/08-bi-visualization.md) |

## Key architectural principles

- **Compute/storage separation** — every engine (Flink, Trino) reads/writes the
  same Iceberg tables in RustFS; none owns the data. The catalog is the only
  shared source of truth for table state.
- **Governed access, not direct S3** — engines never hold long-lived RustFS
  keys. They authenticate to Polaris over OAuth2 and receive *scoped, temporary*
  storage credentials (credential vending). See [04](04-governance-and-security.md).
- **Event-driven, self-driving pipeline** — Flink commits one Iceberg snapshot
  per checkpoint (~1/min); a Dagster sensor watches those commits and
  auto-materializes dbt models. No human presses "run."
- **Medallion tiers** — data is progressively refined `raw → silver → gold`
  as catalog namespaces `raw`, `staging`, `marts`. All Iceberg tables live in the
  `warehouse` bucket; tiers are catalog namespaces.
- **Maintains itself** — scheduled Dagster jobs run Trino maintenance SQL
  (`OPTIMIZE`, `expire_snapshots`, `remove_orphan_files`) to fight the
  small-file problem inherent in streaming lakehouses.
