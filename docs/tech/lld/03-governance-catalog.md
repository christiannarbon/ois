# LLD 03 — Governance & Catalog Layer

**Layer:** Apache Polaris (Iceberg REST Catalog)

## Purpose & scope

Stand up Apache Polaris as the Iceberg REST Catalog with explicit governance:
bootstrap a realm, create a RustFS-backed catalog, define RBAC, and enable
credential vending so downstream engines get scoped, temporary S3 access. **In
scope:** deploy + metastore, realm/root bootstrap, catalog creation, RBAC. **Out
of scope:** any writer or reader engine.

## Component internals

```mermaid
flowchart TB
    subgraph polaris["Polaris (ns: catalog)"]
        rest[[REST API :8181<br/>/api/catalog + /oauth/tokens]]
        realm[Realm<br/>bootstrapped]
        principals[Principals + roles<br/>RBAC grants]
        catalog[Catalog: iceberg<br/>base loc → RustFS]
        vend[Credential vending<br/>STS vs static]
        meta[(Metastore<br/>Postgres/PVC)]
        rest --> realm --> principals --> catalog --> vend
        catalog --- meta
    end
    rustfs[(RustFS<br/>warehouse bucket)]
    vend -.scoped creds.-> rustfs
    catalog -.base storage location.-> rustfs
```

## Configuration contract

| Concern | Setting | Notes |
| :--- | :--- | :--- |
| Deploy | `components/catalog/`, pinned image | persistence backend (Postgres/PVC), not in-memory |
| REST endpoint | `polaris.catalog.svc.cluster.local:8181/api/catalog` | in-cluster URI for engines |
| Token endpoint | OAuth2 **client-credentials** grant | `/oauth/tokens` (realm-scoped) |
| Realm | bootstrapped once, root principal | root only mints engine principals |
| Catalog | name `iceberg`, base storage → RustFS bucket prefix | path-style S3 |
| Vending mode | **STS** (temporary) vs **static** — decided here | the single most important downstream contract |
| RBAC | principals, roles, grants per namespace | least-privilege enforced |

## Key sequence — catalog bootstrap → first authenticated call

```mermaid
sequenceDiagram
    participant O as Operator (scripts)
    participant P as Polaris
    participant M as RustFS
    O->>P: deploy + bootstrap realm (root principal)
    O->>P: create catalog "iceberg" (base loc → RustFS)
    P->>M: validate storage access
    O->>P: create engine principal (client_id/secret) + grants
    O->>P: POST /oauth/tokens (client_credentials)
    P-->>O: access_token
    O->>P: GET namespaces (with token)
    P-->>O: 200 — list namespaces
```

## RBAC model

```mermaid
flowchart LR
    subgraph principals["Principals"]
        root[root]
        flinkP[flink-writer]
        trinoP[trino-reader]
        dbtP[dbt-writer]
    end
    subgraph roles["Roles → grants"]
        wRaw[write: raw ns]
        rAll[read: all ns]
        wSilverGold[write: silver+gold ns]
    end
    root -->|mints| flinkP & trinoP & dbtP
    flinkP --> wRaw
    trinoP --> rAll
    dbtP --> wSilverGold
```

## Inputs consumed / outputs produced

**Consumes (from the [foundation](01-foundation-storage.md)):** cluster,
StorageClass, Helm tooling, RustFS + `warehouse`/`flink` buckets. No dependency on the
ingestion layer.

**Produces — the catalog contract (to stream processing, compute, orchestration
and maintenance):**

- Iceberg REST catalog URI (`http://polaris.catalog.svc.cluster.local:8181/api/catalog`).
- Realm name + OAuth2 token endpoint + client-credentials flow.
- Per-engine `client_id`/`client_secret` (vended via Secret).
- Catalog name (`iceberg`), its RustFS base storage location.
- **Credential-vending mode** (STS vs static) — how engines get scoped S3 creds.

Target folders: `components/catalog/` (Helm values / manifests + metastore),
`scripts/` (idempotent bootstrap/RBAC).
