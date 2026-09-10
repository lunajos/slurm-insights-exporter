package slurm

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Cluster                   string
	Timeout, AccountingWindow time.Duration
	SlowRefresh               time.Duration
	IncludeUsers              bool
	IncludeJobs               bool
}
type Collector struct {
	runner   Runner
	cfg      Config
	slowMu   sync.Mutex
	slowData map[string]slowResult
}

type slowResult struct {
	at      time.Time
	samples []Sample
	jobs    []JobRecord
	err     error
}

func NewCollector(r Runner, cfg Config) *Collector {
	return &Collector{runner: r, cfg: cfg, slowData: map[string]slowResult{}}
}

func (c *Collector) slow(name string, fn func(context.Context) ([]Sample, []JobRecord, error)) func(context.Context) ([]Sample, []JobRecord, error) {
	return func(ctx context.Context) ([]Sample, []JobRecord, error) {
		c.slowMu.Lock()
		old, ok := c.slowData[name]
		c.slowMu.Unlock()
		if ok && time.Since(old.at) < c.cfg.SlowRefresh {
			return old.samples, old.jobs, old.err
		}
		s, j, e := fn(ctx)
		if e != nil && ok {
			return old.samples, old.jobs, e
		}
		c.slowMu.Lock()
		c.slowData[name] = slowResult{time.Now(), s, j, e}
		c.slowMu.Unlock()
		return s, j, e
	}
}

func sample(name, help, typ string, value float64, labels map[string]string) Sample {
	return Sample{Name: "slurm_" + name, Help: help, Type: typ, Value: value, Labels: labels}
}

func (c *Collector) Collect(ctx context.Context) Snapshot {
	start := time.Now()
	snap := Snapshot{CollectedAt: start}
	type result struct {
		s   []Sample
		j   []JobRecord
		err error
	}
	type namedCollector struct {
		name string
		fn   func(context.Context) ([]Sample, []JobRecord, error)
	}
	collectors := []namedCollector{{"nodes", c.nodes}, {"partitions", c.partitions}, {"queue", c.queue}, {"tres_profiles", c.slow("tres_profiles", c.tresProfiles)}, {"reservations", c.slow("reservations", c.reservations)}, {"accounting", c.slow("accounting", c.accounting)}, {"fairshare", c.slow("fairshare", c.fairshare)}, {"scheduler", c.slow("scheduler", c.diagnostics)}, {"licenses", c.slow("licenses", c.licenses)}, {"account_limits", c.slow("account_limits", c.accountLimits)}}
	ch := make(chan result, len(collectors))
	var wg sync.WaitGroup
	for _, item := range collectors {
		wg.Add(1)
		go func(name string, f func(context.Context) ([]Sample, []JobRecord, error)) {
			defer wg.Done()
			started := time.Now()
			x, y, e := f(ctx)
			ok := 1.0
			if e != nil {
				ok = 0
			}
			l := map[string]string{"collector": name}
			x = append(x, sample("exporter_collector_success", "Whether this collector succeeded", "gauge", ok, l), sample("exporter_collector_duration_seconds", "Collector duration", "gauge", time.Since(started).Seconds(), l))
			ch <- result{x, y, e}
		}(item.name, item.fn)
	}
	go func() { wg.Wait(); close(ch) }()
	for r := range ch {
		snap.Samples = append(snap.Samples, r.s...)
		snap.Jobs = append(snap.Jobs, r.j...)
		if r.err != nil {
			snap.Errors = append(snap.Errors, r.err.Error())
		}
	}
	sort.Slice(snap.Samples, func(i, j int) bool { return snap.Samples[i].Name < snap.Samples[j].Name })
	snap.Duration = time.Since(start).Seconds()
	return snap
}

func (c *Collector) partitions(ctx context.Context) ([]Sample, []JobRecord, error) {
	b, e := c.run(ctx, "sinfo", "--noheader", "--summarize", "--format=%P|%a|%l|%D|%C")
	if e != nil {
		return nil, nil, e
	}
	var out []Sample
	for _, r := range lines(b) {
		if len(r) < 5 {
			continue
		}
		l := map[string]string{"cluster": c.cfg.Cluster, "partition": clean(r[0]), "availability": clean(r[1])}
		out = append(out, sample("partition_nodes", "Nodes in partition", "gauge", number(r[3]), l), sample("partition_time_limit_seconds", "Partition time limit", "gauge", duration(r[2]), l))
		cp := strings.Split(r[4], "/")
		if len(cp) == 4 {
			for i, n := range []string{"allocated", "idle", "other", "total"} {
				out = append(out, sample("partition_cpus", "CPUs in partition by disposition", "gauge", number(cp[i]), merge(l, map[string]string{"status": n})))
			}
		}
	}
	return out, nil, nil
}

func (c *Collector) reservations(ctx context.Context) ([]Sample, []JobRecord, error) {
	b, e := c.run(ctx, "scontrol", "show", "reservation", "--oneliner")
	if e != nil {
		return nil, nil, e
	}
	var out []Sample
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		kv := map[string]string{}
		for _, f := range strings.Fields(line) {
			if p := strings.IndexByte(f, '='); p > 0 {
				kv[f[:p]] = f[p+1:]
			}
		}
		if kv["ReservationName"] == "" {
			continue
		}
		l := map[string]string{"cluster": c.cfg.Cluster, "reservation": kv["ReservationName"], "partition": kv["PartitionName"], "state": kv["State"]}
		out = append(out, sample("reservation_info", "Reservation inventory and state", "gauge", 1, l), sample("reservation_nodes", "Reserved nodes", "gauge", number(kv["NodeCnt"]), l), sample("reservation_cores", "Reserved cores", "gauge", number(kv["CoreCnt"]), l))
		if ts, err := time.Parse("2006-01-02T15:04:05", kv["EndTime"]); err == nil {
			out = append(out, sample("reservation_end_time_seconds", "Reservation end as Unix time", "gauge", float64(ts.Unix()), l))
		}
	}
	return out, nil, nil
}

func (c *Collector) run(ctx context.Context, command string, args ...string) ([]byte, error) {
	x, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	return c.runner.Run(x, command, args...)
}

func (c *Collector) nodes(ctx context.Context) ([]Sample, []JobRecord, error) {
	b, e := c.run(ctx, "sinfo", "--noheader", "--Node", "--format=%N|%P|%T|%c|%m|%e|%C|%G|%g|%O|%A")
	if e != nil {
		return nil, nil, e
	}
	var out []Sample
	for _, r := range lines(b) {
		if len(r) < 7 {
			continue
		}
		l := map[string]string{"cluster": c.cfg.Cluster, "node": clean(r[0]), "partition": clean(r[1]), "state": baseState(r[2])}
		out = append(out, sample("node_info", "Node inventory and state", "gauge", 1, l), sample("node_cpus", "Configured CPUs", "gauge", number(r[3]), l), sample("node_memory_bytes", "Configured memory", "gauge", number(r[4])*1024*1024, l), sample("node_free_memory_bytes", "Reported free memory", "gauge", number(r[5])*1024*1024, l))
		cp := strings.Split(r[6], "/")
		if len(cp) == 4 {
			for i, n := range []string{"allocated", "idle", "other", "total"} {
				out = append(out, sample("node_cpu_count", "CPU count by disposition", "gauge", number(cp[i]), merge(l, map[string]string{"status": n})))
			}
		}
		if len(r) >= 9 {
			out = append(out, sample("node_gpus", "GPUs by disposition", "gauge", gpuCount(r[7]), merge(l, map[string]string{"status": "total"})), sample("node_gpus", "GPUs by disposition", "gauge", gpuCount(r[8]), merge(l, map[string]string{"status": "allocated"})))
		}
		if len(r) >= 10 {
			out = append(out, sample("node_cpu_load", "Reported CPU load", "gauge", number(r[9]), l))
		}
		if len(r) >= 11 {
			out = append(out, sample("node_allocated_memory_bytes", "Allocated memory", "gauge", number(r[10])*1024*1024, l))
		}
	}
	return out, nil, nil
}

func (c *Collector) tresProfiles(ctx context.Context) ([]Sample, []JobRecord, error) {
	b, e := c.run(ctx, "scontrol", "show", "nodes", "--oneliner")
	if e != nil {
		return nil, nil, e
	}
	type totals struct{ configured, allocated float64 }
	clusterTotals := map[string]*totals{}
	profileCounts := map[string]float64{}
	profileConfig := map[string]string{}
	profileTRES := map[string]map[string]float64{}
	var out []Sample
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		kv := keyValues(line)
		node := clean(kv["NodeName"])
		if node == "" {
			continue
		}
		cfg := tresNumeric(kv["CfgTRES"])
		alloc := tresNumeric(kv["AllocTRES"])
		normalized := normalizedTRES(cfg)
		sum := sha256.Sum256([]byte(normalized))
		profile := fmt.Sprintf("profile-%x", sum[:6])
		state := baseState(kv["State"])
		partitions := clean(kv["Partitions"])
		profileConfig[profile] = normalized
		profileTRES[profile] = cfg
		profileCounts[strings.Join([]string{profile, state}, "\x00")]++
		out = append(out, sample("node_profile_assignment", "Configured hardware profile assigned to a node", "gauge", 1, map[string]string{"cluster": c.cfg.Cluster, "node": node, "profile": profile, "state": state, "partitions": partitions}))
		keys := map[string]bool{}
		for k := range cfg {
			keys[k] = true
		}
		for k := range alloc {
			keys[k] = true
		}
		for resource := range keys {
			total := cfg[resource]
			used := alloc[resource]
			available := total - used
			if available < 0 {
				available = 0
			}
			l := map[string]string{"cluster": c.cfg.Cluster, "node": node, "resource": resource}
			out = append(out, sample("node_tres", "Per-node trackable resources by disposition", "gauge", total, merge(l, map[string]string{"status": "total"})), sample("node_tres", "Per-node trackable resources by disposition", "gauge", used, merge(l, map[string]string{"status": "allocated"})), sample("node_tres", "Per-node trackable resources by disposition", "gauge", available, merge(l, map[string]string{"status": "available"})))
			if clusterTotals[resource] == nil {
				clusterTotals[resource] = &totals{}
			}
			clusterTotals[resource].configured += total
			clusterTotals[resource].allocated += used
		}
	}
	for resource, t := range clusterTotals {
		available := t.configured - t.allocated
		if available < 0 {
			available = 0
		}
		l := map[string]string{"cluster": c.cfg.Cluster, "resource": resource}
		out = append(out, sample("cluster_tres", "Cluster trackable resources by disposition; memory is bytes", "gauge", t.configured, merge(l, map[string]string{"status": "total"})), sample("cluster_tres", "Cluster trackable resources by disposition; memory is bytes", "gauge", t.allocated, merge(l, map[string]string{"status": "allocated"})), sample("cluster_tres", "Cluster trackable resources by disposition; memory is bytes", "gauge", available, merge(l, map[string]string{"status": "available"})))
	}
	for key, count := range profileCounts {
		p := strings.Split(key, "\x00")
		out = append(out, sample("node_profile_nodes", "Nodes by inferred configured hardware profile and state", "gauge", count, map[string]string{"cluster": c.cfg.Cluster, "profile": p[0], "state": p[1]}))
	}
	for profile, cfg := range profileTRES {
		out = append(out, sample("node_profile_info", "Inferred profile and normalized CfgTRES definition", "gauge", 1, map[string]string{"cluster": c.cfg.Cluster, "profile": profile, "configuration": profileConfig[profile]}))
		for resource, value := range cfg {
			out = append(out, sample("node_profile_tres", "Expected per-node TRES for an inferred hardware profile; memory is bytes", "gauge", value, map[string]string{"cluster": c.cfg.Cluster, "profile": profile, "resource": resource}))
		}
	}
	return out, nil, nil
}

func (c *Collector) queue(ctx context.Context) ([]Sample, []JobRecord, error) {
	b, e := c.run(ctx, "squeue", "--noheader", "--all", "--format=%T|%P|%a|%q|%D|%C|%M|%l|%r|%i|%j|%u|%m|%b|%V|%S|%N")
	if e != nil {
		return nil, nil, e
	}
	type agg struct{ jobs, nodes, cpus, elapsed, limit, memory, gpus float64 }
	m := map[string]*agg{}
	for _, r := range lines(b) {
		if len(r) < 9 {
			continue
		}
		k := strings.Join([]string{baseState(r[0]), clean(r[1]), clean(r[2]), clean(r[3]), clean(r[8])}, "\x00")
		if m[k] == nil {
			m[k] = &agg{}
		}
		a := m[k]
		a.jobs++
		a.nodes += number(r[4])
		a.cpus += number(r[5])
		a.elapsed += duration(r[6])
		a.limit += duration(r[7])
		if len(r) >= 14 {
			a.memory += memoryAllocation(r[12], number(r[5]), number(r[4]))
			a.gpus += gpuCount(r[13])
		}
	}
	var out []Sample
	for k, a := range m {
		p := strings.Split(k, "\x00")
		l := map[string]string{"cluster": c.cfg.Cluster, "state": p[0], "partition": p[1], "account": p[2], "qos": p[3], "reason": p[4]}
		out = append(out, sample("queue_jobs", "Jobs in queue", "gauge", a.jobs, l), sample("queue_nodes", "Nodes requested or allocated", "gauge", a.nodes, l), sample("queue_cpus", "CPUs requested or allocated", "gauge", a.cpus, l), sample("queue_elapsed_seconds", "Aggregate elapsed time", "gauge", a.elapsed, l), sample("queue_time_limit_seconds", "Aggregate time limit", "gauge", a.limit, l))
		out = append(out, sample("queue_memory_bytes", "Memory requested or allocated", "gauge", a.memory, l), sample("queue_gpus", "GPUs requested or allocated", "gauge", a.gpus, l))
	}
	if c.cfg.IncludeJobs {
		for _, r := range lines(b) {
			if len(r) < 17 || baseState(r[0]) != "RUNNING" {
				continue
			}
			l := map[string]string{"cluster": c.cfg.Cluster, "job_id": clean(r[9]), "job_name": clean(r[10]), "user": clean(r[11]), "partition": clean(r[1]), "account": clean(r[2]), "nodelist": clean(r[16])}
			out = append(out, sample("job_info", "Running job information", "gauge", 1, l), sample("job_nodes", "Allocated nodes", "gauge", number(r[4]), l), sample("job_cpus", "Allocated CPUs", "gauge", number(r[5]), l), sample("job_memory_bytes", "Requested memory", "gauge", memoryAllocation(r[12], number(r[5]), number(r[4])), l), sample("job_gpus", "Requested GPUs", "gauge", gpuCount(r[13]), l), sample("job_elapsed_seconds", "Elapsed runtime", "gauge", duration(r[6]), l))
			start, se := time.Parse("2006-01-02T15:04:05", r[15])
			submit, ue := time.Parse("2006-01-02T15:04:05", r[14])
			if se == nil {
				out = append(out, sample("job_start_time_seconds", "Job start Unix time", "gauge", float64(start.Unix()), l))
			}
			if se == nil && ue == nil {
				out = append(out, sample("job_wait_seconds", "Submit-to-start wait", "gauge", start.Sub(submit).Seconds(), l))
			}
		}
	}
	return out, nil, nil
}

func (c *Collector) accounting(ctx context.Context) ([]Sample, []JobRecord, error) {
	start := time.Now().Add(-c.cfg.AccountingWindow).Format("2006-01-02T15:04:05")
	fields := "Cluster,JobIDRaw,User,Account,Partition,QOS,State,AllocCPUS,ElapsedRaw,CPUTimeRAW,TotalCPU,Submit,Start,ReqMem,MaxRSS,ConsumedEnergyRaw,AllocTRES,ReqTRES,AllocNodes"
	b, e := c.run(ctx, "sacct", "--allusers", "--noheader", "--parsable2", "--starttime", start, "--format", fields)
	if e != nil {
		return nil, nil, e
	}
	type agg struct{ jobs, alloc, elapsed, capacity, cpu, memoryUsed, memoryRequested, energy float64 }
	m := map[string]*agg{}
	var jobs []JobRecord
	rows := lines(b)
	maxRSS := map[string]float64{}
	for _, r := range rows {
		if len(r) >= 15 && strings.Contains(r[1], ".") {
			root := strings.SplitN(r[1], ".", 2)[0]
			if v := parseBytes(r[14]); v > maxRSS[root] {
				maxRSS[root] = v
			}
		}
	}
	for _, r := range rows {
		if len(r) < 11 {
			continue
		}
		if strings.Contains(r[1], ".") {
			continue
		}
		st := baseState(r[6])
		k := strings.Join([]string{clean(r[0]), clean(r[3]), clean(r[4]), clean(r[5]), st}, "\x00")
		if m[k] == nil {
			m[k] = &agg{}
		}
		a := m[k]
		a.jobs++
		a.alloc += number(r[7])
		a.elapsed += number(r[8])
		a.capacity += number(r[9])
		a.cpu += duration(r[10])
		j := JobRecord{Cluster: clean(r[0]), JobID: clean(r[1]), Account: clean(r[3]), Partition: clean(r[4]), QOS: clean(r[5]), State: st, AllocCPUs: number(r[7]), ElapsedSeconds: number(r[8]), CPUSeconds: duration(r[10])}
		if len(r) >= 18 {
			nodes := 1.0
			if len(r) >= 19 && number(r[18]) > 0 {
				nodes = number(r[18])
			}
			j.RequestedMemoryBytes = memoryAllocation(r[13], j.AllocCPUs, nodes)
			j.MaxRSSBytes = maxRSS[j.JobID]
			if j.MaxRSSBytes == 0 {
				j.MaxRSSBytes = parseBytes(r[14])
			}
			j.EnergyJoules = number(r[15])
			j.AllocatedTRES = clean(r[16])
			j.RequestedTRES = clean(r[17])
			start, se := time.Parse("2006-01-02T15:04:05", r[12])
			submit, ue := time.Parse("2006-01-02T15:04:05", r[11])
			if se == nil && ue == nil {
				j.WaitSeconds = start.Sub(submit).Seconds()
			}
		}
		capacity := j.AllocCPUs * j.ElapsedSeconds
		if capacity > 0 {
			j.CPUEfficiencyRatio = j.CPUSeconds / capacity
		}
		if j.RequestedMemoryBytes > 0 {
			j.MemoryEfficiencyRatio = j.MaxRSSBytes / j.RequestedMemoryBytes
		}
		a.memoryUsed += j.MaxRSSBytes
		a.memoryRequested += j.RequestedMemoryBytes
		a.energy += j.EnergyJoules
		if c.cfg.IncludeUsers {
			j.User = clean(r[2])
		}
		jobs = append(jobs, j)
	}
	var out []Sample
	for k, a := range m {
		p := strings.Split(k, "\x00")
		l := map[string]string{"cluster": p[0], "account": p[1], "partition": p[2], "qos": p[3], "state": p[4], "window": c.cfg.AccountingWindow.String()}
		for _, x := range []struct {
			n, h string
			v    float64
		}{{"accounting_jobs", "Jobs", a.jobs}, {"accounting_allocated_cpus", "Allocated CPUs", a.alloc}, {"accounting_elapsed_seconds", "Wall-clock seconds", a.elapsed}, {"accounting_cpu_capacity_seconds", "Allocated CPU capacity seconds", a.capacity}, {"accounting_cpu_seconds", "Consumed CPU seconds", a.cpu}} {
			out = append(out, sample(x.n, x.h, "gauge", x.v, l))
		}
		if a.capacity > 0 {
			out = append(out, sample("accounting_cpu_efficiency_ratio", "CPU seconds divided by allocated CPU capacity", "gauge", a.cpu/a.capacity, l))
		}
		out = append(out, sample("accounting_max_rss_bytes", "Sum of job maximum RSS", "gauge", a.memoryUsed, l), sample("accounting_requested_memory_bytes", "Sum of requested job memory", "gauge", a.memoryRequested, l), sample("accounting_energy_joules", "Reported consumed energy", "gauge", a.energy, l))
		if a.memoryRequested > 0 {
			out = append(out, sample("accounting_memory_efficiency_ratio", "Maximum RSS divided by requested memory", "gauge", a.memoryUsed/a.memoryRequested, l))
		}
	}
	return out, jobs, nil
}

func (c *Collector) fairshare(ctx context.Context) ([]Sample, []JobRecord, error) {
	b, e := c.run(ctx, "sshare", "--all", "--parsable2", "--noheader", "--format=Account,User,RawShares,NormShares,RawUsage,EffectvUsage,FairShare")
	if e != nil {
		return nil, nil, e
	}
	var out []Sample
	for _, r := range lines(b) {
		if len(r) < 7 {
			continue
		}
		l := map[string]string{"cluster": c.cfg.Cluster, "account": clean(r[0])}
		if c.cfg.IncludeUsers {
			l["user"] = clean(r[1])
		}
		for i, x := range []struct{ n, h string }{{"fairshare_raw_shares", "Raw shares"}, {"fairshare_normalized_shares", "Normalized shares"}, {"fairshare_raw_usage", "Raw usage"}, {"fairshare_effective_usage", "Effective usage"}, {"fairshare_factor", "Fair-share factor"}} {
			out = append(out, sample(x.n, x.h, "gauge", number(r[i+2]), l))
		}
	}
	return out, nil, nil
}

var diagLine = regexp.MustCompile(`^\s*([^:]+):\s*([0-9.]+)`)

func (c *Collector) diagnostics(ctx context.Context) ([]Sample, []JobRecord, error) {
	b, e := c.run(ctx, "sdiag")
	if e != nil {
		return nil, nil, e
	}
	mapping := map[string]string{"Server thread count": "controller_server_threads", "Agent queue size": "controller_agent_queue_size", "Agent count": "controller_agents", "DBD Agent queue size": "controller_dbd_agent_queue_size", "Jobs submitted": "controller_jobs_submitted_total", "Jobs started": "controller_jobs_started_total", "Jobs completed": "controller_jobs_completed_total", "Jobs canceled": "controller_jobs_canceled_total", "Jobs failed": "controller_jobs_failed_total", "Last queue length": "scheduler_last_queue_length", "Cycles per minute": "scheduler_cycles_per_minute", "Total backfilled jobs (since last slurm start)": "backfill_jobs_total", "Total backfilled jobs (since last stats cycle start)": "backfill_jobs_since_reset", "Last depth cycle": "backfill_last_depth", "Last depth cycle (try sched)": "backfill_last_depth_try_sched", "Depth Mean": "backfill_depth_mean", "Last table size": "backfill_last_table_size", "Mean table size": "backfill_mean_table_size"}
	var out []Sample
	for _, line := range strings.Split(string(b), "\n") {
		m := diagLine.FindStringSubmatch(line)
		if len(m) != 3 {
			continue
		}
		if n, ok := mapping[strings.TrimSpace(m[1])]; ok {
			typ := "gauge"
			if strings.HasSuffix(n, "_total") {
				typ = "counter"
			}
			out = append(out, sample(n, "Slurm controller diagnostic: "+m[1], typ, number(m[2]), map[string]string{"cluster": c.cfg.Cluster}))
		}
	}
	return out, nil, nil
}

func (c *Collector) licenses(ctx context.Context) ([]Sample, []JobRecord, error) {
	b, e := c.run(ctx, "scontrol", "show", "licenses", "--oneliner")
	if e != nil {
		return nil, nil, e
	}
	var out []Sample
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		kv := keyValues(line)
		name := kv["LicenseName"]
		if name == "" {
			name = kv["Name"]
		}
		if name == "" {
			continue
		}
		l := map[string]string{"cluster": c.cfg.Cluster, "license": name, "remote": kv["Remote"]}
		for _, x := range []struct{ key, name string }{{"Total", "total"}, {"Used", "used"}, {"Free", "free"}, {"Reserved", "reserved"}} {
			out = append(out, sample("license_count", "License count by disposition", "gauge", number(kv[x.key]), merge(l, map[string]string{"status": x.name})))
		}
	}
	return out, nil, nil
}

func (c *Collector) accountLimits(ctx context.Context) ([]Sample, []JobRecord, error) {
	b, e := c.run(ctx, "sacctmgr", "--noheader", "--parsable2", "show", "assoc", "where", "user=", "format=Cluster,Account,GrpTRES,GrpJobs,GrpSubmitJobs")
	if e != nil {
		return nil, nil, e
	}
	var out []Sample
	for _, r := range lines(b) {
		if len(r) < 5 || clean(r[1]) == "" {
			continue
		}
		l := map[string]string{"cluster": clean(r[0]), "account": clean(r[1])}
		t := tresValues(r[2])
		out = append(out, sample("account_cpu_limit", "Account CPU limit; zero means unlimited", "gauge", number(t["cpu"]), l), sample("account_memory_limit_bytes", "Account memory limit; zero means unlimited", "gauge", parseBytes(t["mem"]), l), sample("account_running_job_limit", "Account concurrent job limit; zero means unlimited", "gauge", number(r[3]), l), sample("account_submitted_job_limit", "Account submitted job limit; zero means unlimited", "gauge", number(r[4]), l))
	}
	return out, nil, nil
}

func keyValues(line string) map[string]string {
	m := map[string]string{}
	for _, f := range strings.Fields(line) {
		if p := strings.IndexByte(f, '='); p > 0 {
			m[f[:p]] = f[p+1:]
		}
	}
	return m
}
func tresValues(s string) map[string]string {
	m := map[string]string{}
	for _, f := range strings.Split(s, ",") {
		if p := strings.IndexByte(f, '='); p > 0 {
			m[f[:p]] = f[p+1:]
		}
	}
	return m
}

func tresNumeric(s string) map[string]float64 {
	out := map[string]float64{}
	for k, v := range tresValues(s) {
		if p := strings.IndexByte(v, '('); p >= 0 {
			v = v[:p]
		}
		if k == "mem" {
			out[k] = parseBytes(v)
		} else {
			out[k] = leadingNumber(v)
		}
	}
	return out
}

var leadingNumberRE = regexp.MustCompile(`^[0-9.]+`)

func leadingNumber(s string) float64 {
	m := leadingNumberRE.FindString(strings.TrimSpace(s))
	return number(m)
}
func normalizedTRES(m map[string]float64) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%g", k, m[k]))
	}
	return strings.Join(parts, ",")
}

func merge(a, b map[string]string) map[string]string {
	m := map[string]string{}
	for k, v := range a {
		m[k] = v
	}
	for k, v := range b {
		m[k] = v
	}
	return m
}
func (c Config) Validate() error {
	if c.Cluster == "" {
		return fmt.Errorf("cluster must not be empty")
	}
	if c.Timeout <= 0 || c.AccountingWindow <= 0 || c.SlowRefresh <= 0 {
		return fmt.Errorf("durations must be positive")
	}
	return nil
}
