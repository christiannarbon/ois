# ois (open-iceberg-stack)

Easy lakehouse setup for data engineers.

ois sets up a streaming data lakehouse on a local Kubernetes cluster. You describe what you want in one YAML file and ois deploys and connects the pieces for you.

Getting a stack like this running by hand takes a while. Storage, Kafka, an Iceberg catalog, Flink, a query engine and an orchestrator all need to be installed, and most of the effort goes into making them talk to each other. ois takes care of that part so you can spend your time on the data itself.

The stack is fixed and fully open source:

- RustFS for S3-compatible storage
- Kafka for ingestion
- Apache Polaris as the Iceberg catalog
- Flink for streaming data into Iceberg tables
- Trino for queries
- Dagster and dbt for transformations
- Superset for dashboards, if you want them

You choose which parts to run and what goes through them: topics, streams, table maintenance and your own dbt models. If a stream needs more than config can express, you can plug in your own Flink job.

By default everything runs on Minikube and is kept small enough for a laptop. The same config is meant to be deployable to a real cluster later on.

## License

MIT
