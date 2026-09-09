# Grafana dashboards

Eight provisionable dashboards cover the complete Prometheus surface of Slurm Insights Exporter:

1. Cluster Overview
2. Nodes & Partitions
3. Jobs & Queue
4. Accounting & Efficiency
5. Fair Share & Limits
6. Scheduler & Controller
7. Reservations & Licenses
8. Exporter & Audit Health

They require Grafana 10+ and a Prometheus datasource. Provisioning assigns the datasource UID `prometheus`; every dashboard also exposes a datasource selector. Cluster, partition, account, QoS, and node variables support multi-selection.

Run the complete local stack from the repository root:

```bash
docker compose -f deploy/monitoring/docker-compose.yml up -d
```

Grafana is then available on port 3000 and Prometheus on 9090. Change `GRAFANA_ADMIN_PASSWORD` before deploying outside a trusted development environment.

Regenerate dashboards after changing definitions:

```bash
go run ./tools/dashboardgen
```

Per-job panels require `-metrics.per-job`; they intentionally remain empty otherwise. Historical per-job audit records live in JSONL and `/api/v1/jobs`, not Prometheus, so those detailed records need a warehouse datasource for arbitrary long-term job-table exploration.
