package slurm

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakeRunner map[string]string

func (f fakeRunner) Run(_ context.Context, n string, _ ...string) ([]byte, error) {
	v, ok := f[n]
	if !ok {
		return nil, fmt.Errorf("missing %s", n)
	}
	return []byte(v), nil
}
func TestCollect(t *testing.T) {
	r := fakeRunner{
		"sinfo":    "n01|compute*|idle|64|256000|128000|0/64/0/64\n",
		"squeue":   "PENDING|compute|science|normal|1|8|00:10|01:00:00|Resources\n",
		"sacct":    "alpha|42|alice|science|compute|normal|COMPLETED|8|60|480|00:07:30\n",
		"sshare":   "science|alice|100|1|500|0.5|0.8\n",
		"sdiag":    "Server thread count: 4\nJobs submitted: 20\n",
		"scontrol": "ReservationName=maint StartTime=2026-01-01T00:00:00 EndTime=2026-01-01T01:00:00 NodeCnt=2 CoreCnt=128 PartitionName=compute State=ACTIVE\n",
		"sacctmgr": "alpha|science|cpu=100,mem=1T|20|100\n",
	}
	c := NewCollector(r, Config{Cluster: "alpha", Timeout: time.Second, AccountingWindow: 24 * time.Hour, SlowRefresh: 5 * time.Minute})
	s := c.Collect(context.Background())
	if len(s.Errors) != 0 {
		t.Fatal(s.Errors)
	}
	if len(s.Samples) < 15 {
		t.Fatalf("only %d samples", len(s.Samples))
	}
	var b strings.Builder
	RenderPrometheus(&b, s)
	if !strings.Contains(b.String(), `slurm_queue_jobs{account="science"`) {
		t.Fatal(b.String())
	}
	if s.Jobs[0].User != "" {
		t.Fatal("user leaked when disabled")
	}
}

func TestSeffStyleJobEfficiency(t *testing.T) {
	r := fakeRunner{"sacct": "alpha|42|alice|science|compute|normal|COMPLETED|8|60|480|00:07:30|2026-01-01T00:00:00|2026-01-01T00:01:00|2G||10|cpu=8,mem=2G|cpu=8,mem=2G\nalpha|42.batch|alice|science|compute|normal|COMPLETED|8|60|480|00:07:30||||1G|10|cpu=8,mem=2G|cpu=8,mem=2G\n"}
	c := NewCollector(r, Config{Cluster: "alpha", Timeout: time.Second, AccountingWindow: time.Hour, SlowRefresh: time.Minute, IncludeUsers: true})
	_, jobs, err := c.accounting(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs=%d", len(jobs))
	}
	j := jobs[0]
	if j.CPUEfficiencyRatio != 0.9375 {
		t.Fatalf("cpu efficiency=%v", j.CPUEfficiencyRatio)
	}
	if j.MemoryEfficiencyRatio != 0.5 {
		t.Fatalf("memory efficiency=%v", j.MemoryEfficiencyRatio)
	}
	if j.WaitSeconds != 60 {
		t.Fatalf("wait=%v", j.WaitSeconds)
	}
}
