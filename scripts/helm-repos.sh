#!/usr/bin/env bash
set -euo pipefail

if ! command -v helm >/dev/null 2>&1; then
  echo "ERROR: 'helm' binary not found on PATH. Please install Helm >= 3.12." >&2
  exit 1
fi

echo "======================================================================="
echo "Registering & Updating Helm Repositories for open-iceberg-stack"
echo "======================================================================="

# Helm Repositories: <repo-name> <repo-url>
# Structured so subsequent epics can append their repositories here cleanly.
helm repo add minio https://charts.min.io/ --force-update
helm repo add bitnami https://charts.bitnami.com/bitnami --force-update
helm repo add strimzi https://strimzi.io/charts/ --force-update
helm repo add trino https://trinodb.github.io/charts/ --force-update
helm repo add apache-airflow https://airflow.apache.org --force-update

echo "======================================================================="
echo "Updating registered Helm repositories..."
helm repo update
echo "======================================================================="
echo "Helm repository registration complete."
