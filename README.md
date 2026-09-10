# Slurm Insights Exporter

A dependency-free Go service that turns Slurm controller and `slurmdbd` accounting data into Prometheus metrics and a BI-friendly JSON snapshot.

It runs the standard Slurm CLI tools concurrently, caches one snapshot for all consumers, and never talks directly to the accounting database. This preserves Slurm's authentication and `PrivateData` policy.

## Coverage

| Domain | Source | Examples |
|---|---|---|
| Nodes | `sinfo` | state, CPUs, allocated/idle/other CPUs, configured/free memory |
| Partitions | `sinfo` | availability, nodes, CPU disposition, time limit |
| Live queue | `squeue` | jobs, nodes, CPUs, elapsed/time-limit seconds by state, partition, account, QoS and reason |
| Accounting | `sacct` / `slurmdbd` | jobs, allocated CPUs, wall time, CPU capacity, consumed CPU and efficiency by account/state/partition/QoS |
| Fair share | `sshare` | raw/normalized shares, raw/effective usage and fair-share factor |
| Reservations | `scontrol` | state, node/core counts and end time |
| Controller | `sdiag` | threads, agent/DBD queues and submitted/started/completed/canceled/failed counters |
| Exporter | internal | health, collection success, errors and duration |
| GPUs and licenses | `sinfo` / `scontrol` | per-node GPU allocation and license total/used/free/reserved |
| Account limits | `sacctmgr` | CPU, memory, running-job and submitted-job limits |

Prometheus data is aggregated where cardinality would otherwise grow without bound. `/api/v1/snapshot` includes job records for ingestion into ClickHouse, DuckDB, BigQuery, Snowflake, Superset, Metabase or a lakehouse. User names are omitted by default because they are PII and a high-cardinality dimension.

## Quick start

Requirements: Go 1.23+ and working `sinfo`, `squeue`, `sacct`, `sshare`, `scontrol`, and `sdiag` commands for the service account.

```bash
make test build
SLURM_CLUSTER=alpha ./bin/slurm-insights-exporter
curl http://localhost:9341/metrics
curl http://localhost:9341/api/v1/snapshot
```

### RPM installation

Download the Linux x86_64 RPM matching EL8-compatible or EL9-compatible systems from the GitHub release, then:

```bash
sudo rpm -Uvh slurm-insights-exporter-0.1.0-1*.x86_64.rpm
sudo editor /etc/sysconfig/slurm-insights-exporter
sudo systemctl enable --now slurm-insights-exporter
systemctl status slurm-insights-exporter
```

The RPM expects an existing `slurm` service account with permission to run the Slurm CLI and read the desired accounting data. Installation does not start the daemon automatically. Configuration upgrades preserve local changes through RPM's `noreplace` behavior.

The `.el8` package targets RHEL/Rocky/AlmaLinux 8 and the `.el9` package targets RHEL/Rocky/AlmaLinux 9. Both contain the same statically linked Linux x86_64 Go binary; separate RPMs provide explicit distribution targeting and repository compatibility.

Prometheus configuration:

```yaml
scrape_configs:
  - job_name: slurm
    scrape_interval: 30s
    static_configs:
      - targets: ["slurm-controller:9341"]
```

Install `deploy/prometheus-rules.yaml` for utilization, availability, queue depth, throughput, failure-rate, and account-efficiency recording rules and alerts.

## Grafana dashboards

Eight generated and provisionable dashboards with 65 panels are included under `deploy/grafana`:

- Cluster Overview
- Nodes & Partitions
- Jobs & Queue
- Accounting & seff-style Efficiency
- Fair Share & Account Limits
- Scheduler & Controller
- Reservations & Licenses
- Exporter & Audit Health

All PromQL uses this exporter's actual metric and label schema. Cluster totals deduplicate nodes that belong to multiple partitions. Variables support datasource, cluster, partition, account, QoS, node, arbitrary TRES, and inferred node-profile selection. See `deploy/grafana/README.md` for provisioning and limitations.

For a turnkey development stack:

```bash
GRAFANA_ADMIN_PASSWORD='change-me' \
  docker compose -f deploy/monitoring/docker-compose.yml up -d
```

This starts the exporter, Prometheus with 90-day retention and recording rules, and Grafana with every dashboard preloaded. Production installations should use durable storage, authentication, TLS, and a Prometheus retention policy sized for their cluster.

Each GitHub release includes a standalone `grafana_dashboards` tarball containing the dashboard JSON, Grafana provisioning, Prometheus rules, and turnkey monitoring example. It can be deployed without installing the exporter RPM on the Grafana host.

## Configuration

| Flag | Environment | Default | Purpose |
|---|---|---:|---|
| `-web.listen-address` | `LISTEN_ADDRESS` | `:9341` | HTTP bind address |
| `-slurm.cluster` | `SLURM_CLUSTER` | `default` | Stable cluster label |
| `-slurm.timeout` | `SLURM_TIMEOUT` | `15s` | Timeout for each command |
| `-accounting.window` | `ACCOUNTING_WINDOW` | `24h` | Rolling `sacct` lookback |
| `-cache.ttl` | `CACHE_TTL` | `30s` | Minimum interval between collections |
| `-cache.slow-ttl` | `SLOW_CACHE_TTL` | `5m` | Refresh interval for `sacct`, `sacctmgr`, `sshare`, `sdiag`, licenses and reservations |
| `-labels.users` | — | `false` | Include user dimension in BI jobs/fair share |
| `-metrics.per-job` | — | `false` | Running-job metrics with job/user/nodelist labels for Grafana joins |
| `-history.dir` | `HISTORY_DIR` | disabled | Directory for durable JSONL observations and job events |
| `-history.interval` | `HISTORY_INTERVAL` | `5m` | Durable observation frequency |

Enabling per-job metrics adds `slurm_job_info`, nodes, CPUs, memory, GPUs, elapsed time, start time and queue wait. This supports joins to node-exporter and DCGM data using the `nodelist` dimension, but it should only be enabled after estimating series cardinality.

Endpoints are `/metrics`, `/api/v1/snapshot`, `/api/v1/jobs`, and `/-/healthy`. A partial snapshot remains available when one command fails; `slurm_exporter_up` becomes `0` and errors appear as `# ERROR` comments in Prometheus output and in the JSON `errors` field.

### Jobs and seff-style efficiency

`GET /api/v1/jobs?state=COMPLETED&account=science&limit=1000` returns a bounded, filterable job list. The maximum limit is 10,000. Every record includes CPU efficiency (`TotalCPU / (AllocCPUS × Elapsed)`), memory efficiency (`MaxRSS / requested memory`), queue wait, energy, and requested/allocated TRES when Slurm accounting provides them. Step rows are used to find MaxRSS but are not double-counted as jobs. These are the core calculations presented by Slurm's `seff` contrib tool; exact memory quality still depends on the configured job accounting plugin and sampling interval.

Prometheus exports the same information as account/partition/QoS/state aggregates: `slurm_accounting_cpu_efficiency_ratio`, `slurm_accounting_memory_efficiency_ratio`, requested memory, maximum RSS and energy. Individual historical job IDs stay out of Prometheus.

### Load-control design

- Fast tier (`sinfo`, `squeue`): refreshed at `cache.ttl`, normally 30 seconds.
- Slow tier (`sacct`, `sacctmgr`, `sshare`, `sdiag`, reservations and licenses): refreshed at `cache.slow-ttl`, normally five minutes.
- A failed slow refresh serves the last good data and is retried on the next fast refresh.
- Concurrent HTTP requests share one snapshot; they do not launch duplicate commands.
- The job endpoint reads the cache and never performs its own Slurm query.

For very large clusters, increase the slow TTL to 15 minutes, shorten the accounting window, leave per-job Prometheus metrics disabled, and export completed jobs incrementally to a warehouse.

## Historical analytics and audit

Enable the built-in archive when you need durable evidence in addition to Prometheus:

```bash
./bin/slurm-insights-exporter \
  -history.dir=/var/lib/slurm-insights/history \
  -history.interval=5m
```

The archive creates UTC daily JSONL files. It writes:

- periodic metric observations for trend analysis, forecasting, anomaly detection and incident reconstruction;
- `job_state_change` events only when a job is new or its accounting fields change, avoiding repeated copies of the same completed job;
- sequence numbers, previous hashes and SHA-256 hashes, forming a tamper-evident chain suitable for audit verification;
- a durable state index so deduplication and the chain continue after restart.

`GET /api/v1/history/status` reports the sequence, chain head, tracked-job count and last successful write. Disk writes run on their own timer, never on the Prometheus request path. Protect this directory with filesystem permissions, backups and immutable/WORM storage when it is an audit record.

For an actual production data plane, use all three layers:

1. Prometheus or VictoriaMetrics for fast operational graphs and alerts.
2. The append-only archive as replayable raw evidence.
3. ClickHouse, TimescaleDB, BigQuery or another warehouse fed from JSONL for multi-year capacity prediction, cost allocation and BI.

Useful historical investigations include queue-wait percentiles, CPU/memory efficiency distributions, failure cohorts, fair-share drift, node drain chronology, license saturation, demand seasonality and account-level capacity forecasting.

## BI model

Poll `/api/v1/snapshot` on a schedule and append job rows keyed by `(cluster, job_id)`. Upsert rather than blindly append because active jobs change state. Recommended dimensions are time, cluster, account, partition, QoS, state, and optionally user. Useful measures include:

- CPU efficiency: `cpu_seconds / (alloc_cpus * elapsed_seconds)`
- throughput and failure/cancellation rate by account or partition
- queue wait, runtime, allocation, and capacity trends
- fair-share allocation versus effective usage
- idle capacity and saturation by partition

Prometheus is the right store for operational time series. A warehouse is the right store for per-job history, chargeback, forecasting, cohort analysis, and long retention. Never add job ID or user labels to the Prometheus metrics without a strict retention/cardinality plan.

## Permissions and deployment

For complete accounting, run as `SlurmUser`, root, or an account allowed to see all jobs; `PrivateData` can restrict `sacct`. The systemd unit expects the binary at `/usr/local/bin/slurm-insights-exporter` and optional settings in `/etc/slurm-insights-exporter.env`.

The container needs Slurm configuration, Munge credentials/socket, network access, and compatible Slurm client packages from your site. The sample Kubernetes manifest is therefore a starting point, not a complete authentication setup.

## Scope and compatibility

This initial backend uses stable CLI output fields, making it useful across many Slurm releases. Exact command field support varies by site/version. Run the service against a staging controller before production. The collector cache is important because Slurm warns against excessive synchronous controller/accounting queries.

The architecture intentionally leaves room for a versioned `slurmrestd` backend. Modern Slurm also offers native `/metrics` endpoints; use those alongside or instead of overlapping controller metrics when available, while retaining this exporter for accounting and BI projections.

## Development

```bash
make fmt test vet build
```

The project is MIT licensed.
