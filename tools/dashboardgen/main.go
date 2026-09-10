package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type query struct{ Expr, Legend string }
type panelDef struct {
	Title, Type, Unit string
	Queries           []query
	Description       string
}
type dashboardDef struct {
	File, UID, Title string
	Panels           []panelDef
}

func main() {
	out := "deploy/grafana/dashboards"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		panic(err)
	}
	for _, d := range dashboards() {
		b, err := json.MarshalIndent(build(d), "", "  ")
		if err != nil {
			panic(err)
		}
		b = append(b, '\n')
		if err = os.WriteFile(filepath.Join(out, d.File), b, 0644); err != nil {
			panic(err)
		}
	}
}

func dashboards() []dashboardDef {
	return []dashboardDef{
		{"overview.json", "slurm-overview", "Slurm / Cluster Overview", []panelDef{
			{"Exporter Up", "stat", "none", q(`min(slurm_exporter_up)`, `Exporter`), "All collectors succeeded on the latest snapshot."},
			{"CPU Utilization", "gauge", "percent", q(`100 * sum(max by (cluster, node) (slurm_node_cpu_count{cluster=~"$cluster",status="allocated"})) / clamp_min(sum(max by (cluster, node) (slurm_node_cpu_count{cluster=~"$cluster",status="total"})), 1)`, `CPU`), "Allocated CPUs as a percentage of configured CPUs, deduplicated for nodes in multiple partitions."},
			{"Memory Utilization", "gauge", "percent", q(`100 * sum(max by (cluster, node) (slurm_node_allocated_memory_bytes{cluster=~"$cluster"})) / clamp_min(sum(max by (cluster, node) (slurm_node_memory_bytes{cluster=~"$cluster"})), 1)`, `Memory`), "Slurm allocated memory divided by configured memory, deduplicated by node."},
			{"GPU Allocation", "gauge", "percent", q(`100 * sum(max by (cluster, node) (slurm_node_gpus{cluster=~"$cluster",status="allocated"})) / clamp_min(sum(max by (cluster, node) (slurm_node_gpus{cluster=~"$cluster",status="total"})), 1)`, `GPU`), "Allocated GPUs divided by configured GPUs, deduplicated by node."},
			{"Jobs by State", "timeseries", "short", q(`sum by (state) (slurm_queue_jobs{cluster=~"$cluster"})`, `{{state}}`), "Live queue state history."},
			{"Nodes by State", "timeseries", "short", q(`sum by (state) (max by (cluster, node, state) (slurm_node_info{cluster=~"$cluster"}))`, `{{state}}`), "Node state history, deduplicated for multi-partition membership."},
			{"CPU Disposition", "timeseries", "short", q(`sum by (status) (max by (cluster, node, status) (slurm_node_cpu_count{cluster=~"$cluster"}))`, `{{status}}`), "Allocated, idle, other, and total CPUs, deduplicated by node."},
			{"Pending Reasons", "bargauge", "short", q(`sum by (reason) (slurm_queue_jobs{cluster=~"$cluster",state="PENDING"})`, `{{reason}}`), "Current pending jobs grouped by Slurm reason."},
			{"Total CPUs", "stat", "short", q(`sum(slurm_cluster_tres{cluster=~"$cluster",resource="cpu",status="total"})`, `total`), "Configured CPU TRES across the cluster."},
			{"Available CPUs", "stat", "short", q(`sum(slurm_cluster_tres{cluster=~"$cluster",resource="cpu",status="available"})`, `available`), "Configured CPU TRES minus allocated CPU TRES."},
			{"Available Memory", "stat", "bytes", q(`sum(slurm_cluster_tres{cluster=~"$cluster",resource="mem",status="available"})`, `available`), "Configured memory TRES minus allocated memory TRES."},
			{"All Trackable Resources", "table", "short", q(`sum by (resource, status) (slurm_cluster_tres{cluster=~"$cluster",resource=~"$resource"})`, `{{resource}} / {{status}}`), "All CfgTRES and AllocTRES resources. Memory values are bytes; other resources use native Slurm counts."},
		}},
		{"capacity.json", "slurm-capacity", "Slurm / Nodes & Partitions", []panelDef{
			{"Partition CPUs", "timeseries", "short", q(`sum by (partition, status) (slurm_partition_cpus{cluster=~"$cluster",partition=~"$partition"})`, `{{partition}} / {{status}}`), "CPU disposition by partition."},
			{"Partition Nodes", "timeseries", "short", q(`sum by (partition) (slurm_partition_nodes{cluster=~"$cluster",partition=~"$partition"})`, `{{partition}}`), "Node capacity by partition."},
			{"Node CPU Load", "timeseries", "none", q(`slurm_node_cpu_load{cluster=~"$cluster",partition=~"$partition",node=~"$node"}`, `{{node}}`), "Slurm-reported load per selected node."},
			{"Node Free Memory", "timeseries", "bytes", q(`slurm_node_free_memory_bytes{cluster=~"$cluster",partition=~"$partition",node=~"$node"}`, `{{node}}`), "Reported free memory."},
			{"Node Allocated Memory", "timeseries", "bytes", q(`slurm_node_allocated_memory_bytes{cluster=~"$cluster",partition=~"$partition",node=~"$node"}`, `{{node}}`), "Memory assigned by Slurm."},
			{"Node GPUs", "timeseries", "short", q(`slurm_node_gpus{cluster=~"$cluster",partition=~"$partition",node=~"$node"}`, `{{node}} / {{status}}`), "Total and allocated GPUs."},
			{"Partition Time Limits", "table", "s", q(`max by (partition) (slurm_partition_time_limit_seconds{cluster=~"$cluster",partition=~"$partition"})`, `{{partition}}`), "Configured partition time limits."},
			{"Node Status Timeline", "state-timeline", "none", q(`slurm_node_profile_assignment{cluster=~"$cluster",partitions=~".*$partition.*",node=~"$node"}`, `{{node}} / {{state}}`), "Node state and profile assignment over time."},
			{"Node Profile Membership", "table", "none", q(`slurm_node_profile_assignment{cluster=~"$cluster",profile=~"$profile",node=~"$node"}`, `{{node}} / {{profile}} / {{state}}`), "Expected node shape inferred from normalized Slurm CfgTRES."},
			{"Profile Population by State", "bargauge", "short", q(`sum by (profile, state) (slurm_node_profile_nodes{cluster=~"$cluster",profile=~"$profile"})`, `{{profile}} / {{state}}`), "Highlights unusual or singleton node profiles and unhealthy states."},
			{"Expected TRES per Profile", "table", "short", q(`slurm_node_profile_tres{cluster=~"$cluster",profile=~"$profile",resource=~"$resource"}`, `{{profile}} / {{resource}}`), "Expected per-node resources derived from CfgTRES. Memory values are bytes."},
		}},
		{"jobs.json", "slurm-jobs", "Slurm / Jobs & Queue", []panelDef{
			{"Queue Jobs", "timeseries", "short", q(`sum by (state) (slurm_queue_jobs{cluster=~"$cluster",partition=~"$partition",account=~"$account",qos=~"$qos"})`, `{{state}}`), "Jobs across all live states."},
			{"Queue CPUs", "timeseries", "short", q(`sum by (state) (slurm_queue_cpus{cluster=~"$cluster",partition=~"$partition",account=~"$account",qos=~"$qos"})`, `{{state}}`), "Requested or allocated CPUs."},
			{"Queue GPUs", "timeseries", "short", q(`sum by (state) (slurm_queue_gpus{cluster=~"$cluster",partition=~"$partition",account=~"$account",qos=~"$qos"})`, `{{state}}`), "Requested or allocated GPUs."},
			{"Queue Memory", "timeseries", "bytes", q(`sum by (state) (slurm_queue_memory_bytes{cluster=~"$cluster",partition=~"$partition",account=~"$account",qos=~"$qos"})`, `{{state}}`), "Requested or allocated memory."},
			{"Pending by Reason", "bargauge", "short", q(`sum by (reason) (slurm_queue_jobs{cluster=~"$cluster",partition=~"$partition",account=~"$account",state="PENDING"})`, `{{reason}}`), "Resource, priority, dependency, and policy pressure."},
			{"Running Job CPU", "table", "short", q(`slurm_job_cpus{cluster=~"$cluster",partition=~"$partition",account=~"$account"}`, `{{job_id}} {{job_name}} {{user}}`), "Requires -metrics.per-job."},
			{"Running Job GPU", "table", "short", q(`slurm_job_gpus{cluster=~"$cluster",partition=~"$partition",account=~"$account"}`, `{{job_id}} {{job_name}} {{user}}`), "Requires -metrics.per-job."},
			{"Running Job Wait", "table", "s", q(`slurm_job_wait_seconds{cluster=~"$cluster",partition=~"$partition",account=~"$account"}`, `{{job_id}} {{job_name}} {{user}}`), "Submit-to-start time; requires -metrics.per-job."},
		}},
		{"accounting.json", "slurm-accounting", "Slurm / Accounting & Efficiency", []panelDef{
			{"Weighted CPU Efficiency", "gauge", "percent", q(`100 * sum(slurm_accounting_cpu_seconds{cluster=~"$cluster",partition=~"$partition",account=~"$account",qos=~"$qos"}) / clamp_min(sum(slurm_accounting_cpu_capacity_seconds{cluster=~"$cluster",partition=~"$partition",account=~"$account",qos=~"$qos"}), 1)`, `CPU efficiency`), "seff-style consumed CPU divided by allocated core-walltime."},
			{"Weighted Memory Efficiency", "gauge", "percent", q(`100 * sum(slurm_accounting_max_rss_bytes{cluster=~"$cluster",partition=~"$partition",account=~"$account",qos=~"$qos"}) / clamp_min(sum(slurm_accounting_requested_memory_bytes{cluster=~"$cluster",partition=~"$partition",account=~"$account",qos=~"$qos"}), 1)`, `Memory efficiency`), "Maximum RSS divided by requested memory."},
			{"Jobs by Outcome", "timeseries", "short", q(`sum by (state) (slurm_accounting_jobs{cluster=~"$cluster",partition=~"$partition",account=~"$account",qos=~"$qos"})`, `{{state}}`), "Jobs within the configured accounting window."},
			{"CPU Efficiency by Account", "bargauge", "percentunit", q(`sum by (account) (slurm_accounting_cpu_seconds{cluster=~"$cluster",account=~"$account"}) / clamp_min(sum by (account) (slurm_accounting_cpu_capacity_seconds{cluster=~"$cluster",account=~"$account"}), 1)`, `{{account}}`), "Weighted account efficiency."},
			{"Memory Efficiency by Account", "bargauge", "percentunit", q(`sum by (account) (slurm_accounting_max_rss_bytes{cluster=~"$cluster",account=~"$account"}) / clamp_min(sum by (account) (slurm_accounting_requested_memory_bytes{cluster=~"$cluster",account=~"$account"}), 1)`, `{{account}}`), "Weighted memory efficiency."},
			{"Energy by Account", "timeseries", "joule", q(`sum by (account) (slurm_accounting_energy_joules{cluster=~"$cluster",account=~"$account"})`, `{{account}}`), "Accounting-window energy; requires energy accounting."},
			{"CPU Capacity Seconds", "timeseries", "s", q(`sum by (account) (slurm_accounting_cpu_capacity_seconds{cluster=~"$cluster",account=~"$account"})`, `{{account}}`), "Allocated core-walltime."},
		}},
		{"policy.json", "slurm-policy", "Slurm / Fair Share & Limits", []panelDef{
			{"Fair-share Factor", "timeseries", "none", q(`slurm_fairshare_factor{cluster=~"$cluster",account=~"$account"}`, `{{account}} {{user}}`), "Fair-share factor by association."},
			{"Normalized Shares vs Effective Usage", "timeseries", "percentunit", []query{{`slurm_fairshare_normalized_shares{cluster=~"$cluster",account=~"$account"}`, `shares {{account}} {{user}}`}, {`slurm_fairshare_effective_usage{cluster=~"$cluster",account=~"$account"}`, `usage {{account}} {{user}}`}}, "Allocation versus effective usage."},
			{"Raw Usage", "timeseries", "short", q(`slurm_fairshare_raw_usage{cluster=~"$cluster",account=~"$account"}`, `{{account}} {{user}}`), "Raw association usage."},
			{"Account CPU Limits", "table", "short", q(`slurm_account_cpu_limit{cluster=~"$cluster",account=~"$account"}`, `{{account}}`), "Zero means unlimited."},
			{"Account Memory Limits", "table", "bytes", q(`slurm_account_memory_limit_bytes{cluster=~"$cluster",account=~"$account"}`, `{{account}}`), "Zero means unlimited."},
			{"Account Job Limits", "table", "short", []query{{`slurm_account_running_job_limit{cluster=~"$cluster",account=~"$account"}`, `running {{account}}`}, {`slurm_account_submitted_job_limit{cluster=~"$cluster",account=~"$account"}`, `submitted {{account}}`}}, "Concurrent and submitted job limits."},
		}},
		{"scheduler.json", "slurm-scheduler", "Slurm / Scheduler & Controller", []panelDef{
			{"Server Threads", "timeseries", "short", q(`slurm_controller_server_threads{cluster=~"$cluster"}`, `threads`), "Active slurmctld server threads."},
			{"Agent Queues", "timeseries", "short", []query{{`slurm_controller_agent_queue_size{cluster=~"$cluster"}`, `agent`}, {`slurm_controller_dbd_agent_queue_size{cluster=~"$cluster"}`, `DBD agent`}}, "Controller agent and database delivery backlogs."},
			{"Scheduler Queue Length", "timeseries", "short", q(`slurm_scheduler_last_queue_length{cluster=~"$cluster"}`, `queue`), "Jobs examined in the latest scheduler queue."},
			{"Cycles per Minute", "timeseries", "short", q(`slurm_scheduler_cycles_per_minute{cluster=~"$cluster"}`, `cycles/min`), "Scheduler activity."},
			{"Job Event Rates", "timeseries", "ops", []query{{`rate(slurm_controller_jobs_submitted_total{cluster=~"$cluster"}[5m])`, `submitted`}, {`rate(slurm_controller_jobs_started_total{cluster=~"$cluster"}[5m])`, `started`}, {`rate(slurm_controller_jobs_completed_total{cluster=~"$cluster"}[5m])`, `completed`}, {`rate(slurm_controller_jobs_failed_total{cluster=~"$cluster"}[5m])`, `failed`}}, "Controller counters converted to rates."},
			{"Backfill Jobs", "timeseries", "short", q(`slurm_backfill_jobs_total{cluster=~"$cluster"}`, `jobs`), "Jobs started through backfill."},
			{"Backfill Depth", "timeseries", "short", []query{{`slurm_backfill_last_depth{cluster=~"$cluster"}`, `last depth`}, {`slurm_backfill_last_depth_try_sched{cluster=~"$cluster"}`, `try sched`}, {`slurm_backfill_depth_mean{cluster=~"$cluster"}`, `mean`}}, "Backfill work depth."},
		}},
		{"resources.json", "slurm-resources", "Slurm / Reservations & Licenses", []panelDef{
			{"Reservations", "table", "none", q(`slurm_reservation_info{cluster=~"$cluster"}`, `{{reservation}} {{partition}} {{state}}`), "Reservation inventory."},
			{"Reserved Nodes", "timeseries", "short", q(`slurm_reservation_nodes{cluster=~"$cluster"}`, `{{reservation}}`), "Node allocation by reservation."},
			{"Reserved Cores", "timeseries", "short", q(`slurm_reservation_cores{cluster=~"$cluster"}`, `{{reservation}}`), "Core allocation by reservation."},
			{"Reservation End", "table", "dateTimeAsIso", q(`slurm_reservation_end_time_seconds{cluster=~"$cluster"} * 1000`, `{{reservation}}`), "Reservation expiry."},
			{"Licenses", "timeseries", "short", q(`slurm_license_count{cluster=~"$cluster"}`, `{{license}} / {{status}}`), "Total, used, free, and reserved license counts."},
			{"License Utilization", "bargauge", "percent", q(`100 * sum by (license) (slurm_license_count{cluster=~"$cluster",status="used"}) / clamp_min(sum by (license) (slurm_license_count{cluster=~"$cluster",status="total"}), 1)`, `{{license}}`), "Current license saturation."},
		}},
		{"health.json", "slurm-health", "Slurm / Exporter & Audit Health", []panelDef{
			{"Exporter Up", "stat", "none", q(`slurm_exporter_up`, `up`), "One only when all collectors succeed."},
			{"Collection Duration", "timeseries", "s", q(`slurm_exporter_collection_duration_seconds`, `snapshot`), "Total uncached collection time."},
			{"Collector Success", "timeseries", "none", q(`slurm_exporter_collector_success`, `{{collector}}`), "Per-collector health."},
			{"Collector Duration", "timeseries", "s", q(`slurm_exporter_collector_duration_seconds`, `{{collector}}`), "Per-collector latency; cached slow collectors approach zero."},
			{"History Enabled", "stat", "none", q(`slurm_history_enabled`, `history`), "Append-only history configuration state."},
			{"Audit Sequence", "stat", "short", q(`slurm_history_sequence`, `sequence`), "Latest hash-chain sequence."},
			{"Tracked Jobs", "stat", "short", q(`slurm_history_tracked_jobs`, `jobs`), "Jobs indexed by the audit archive."},
			{"Archive Write Age", "stat", "s", q(`time() - slurm_history_last_write_timestamp_seconds`, `age`), "Seconds since the last successful archive write."},
		}},
	}
}

func q(expr, legend string) []query { return []query{{expr, legend}} }
func build(d dashboardDef) map[string]any {
	panels := []any{}
	for i, p := range d.Panels {
		targets := []any{}
		for j, t := range p.Queries {
			targets = append(targets, map[string]any{"refId": string(rune('A' + j)), "expr": t.Expr, "legendFormat": t.Legend, "range": p.Type != "table", "instant": p.Type == "table"})
		}
		panels = append(panels, map[string]any{"id": i + 1, "title": p.Title, "type": p.Type, "description": p.Description, "datasource": map[string]any{"type": "prometheus", "uid": "${DS_PROMETHEUS}"}, "gridPos": map[string]any{"h": 8, "w": 6, "x": (i % 4) * 6, "y": (i / 4) * 8}, "fieldConfig": map[string]any{"defaults": map[string]any{"unit": p.Unit}, "overrides": []any{}}, "options": options(p.Type), "targets": targets})
	}
	return map[string]any{"id": nil, "uid": d.UID, "title": d.Title, "description": "Generated for slurm-insights-exporter metrics.", "tags": []string{"slurm", "hpc", "slurm-insights-exporter"}, "timezone": "browser", "schemaVersion": 39, "version": 1, "refresh": "30s", "time": map[string]any{"from": "now-24h", "to": "now"}, "templating": map[string]any{"list": variables()}, "annotations": map[string]any{"list": []any{}}, "panels": panels}
}
func options(t string) map[string]any {
	if t == "stat" || t == "gauge" {
		return map[string]any{"reduceOptions": map[string]any{"calcs": []string{"lastNotNull"}, "values": false}, "orientation": "auto"}
	}
	if t == "bargauge" {
		return map[string]any{"reduceOptions": map[string]any{"calcs": []string{"lastNotNull"}, "values": false}, "orientation": "horizontal", "displayMode": "gradient"}
	}
	if t == "table" {
		return map[string]any{"showHeader": true}
	}
	return map[string]any{"legend": map[string]any{"displayMode": "table", "placement": "bottom"}, "tooltip": map[string]any{"mode": "multi"}}
}
func variables() []any {
	return []any{variable("DS_PROMETHEUS", "Datasource", "datasource", "prometheus", ""), variable("cluster", "Cluster", "query", `label_values(slurm_node_info, cluster)`, ".*"), variable("partition", "Partition", "query", `label_values(slurm_node_info{cluster=~"$cluster"}, partition)`, ".*"), variable("account", "Account", "query", `label_values(slurm_queue_jobs{cluster=~"$cluster"}, account)`, ".*"), variable("qos", "QoS", "query", `label_values(slurm_queue_jobs{cluster=~"$cluster"}, qos)`, ".*"), variable("node", "Node", "query", `label_values(slurm_node_info{cluster=~"$cluster",partition=~"$partition"}, node)`, ".*"), variable("resource", "TRES", "query", `label_values(slurm_cluster_tres{cluster=~"$cluster"}, resource)`, ".*"), variable("profile", "Node Profile", "query", `label_values(slurm_node_profile_info{cluster=~"$cluster"}, profile)`, ".*")}
}
func variable(name, label, typ, query, all string) map[string]any {
	v := map[string]any{"name": name, "label": label, "type": typ, "query": query, "refresh": 1}
	if typ == "datasource" {
		v["current"] = map[string]any{"text": "Prometheus", "value": "prometheus"}
		return v
	}
	v["includeAll"] = true
	v["allValue"] = all
	v["multi"] = true
	return v
}
