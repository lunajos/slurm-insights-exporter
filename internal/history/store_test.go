package history

import (
	"bufio"
	"encoding/json"
	"github.com/raging-racoons/slurm-insights-exporter/internal/slurm"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendSnapshotDeduplicatesJobs(t *testing.T) {
	d := t.TempDir()
	s, err := Open(d)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	snap := slurm.Snapshot{CollectedAt: at, Samples: []slurm.Sample{{Name: "slurm_up", Value: 1}}, Jobs: []slurm.JobRecord{{Cluster: "a", JobID: "42", State: "RUNNING"}}}
	if err = s.AppendSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	if err = s.AppendSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(d, "2026-01-02.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	count := 0
	last := ""
	for scan.Scan() {
		var e Envelope
		if err = json.Unmarshal(scan.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		if count > 0 && e.PreviousHash != last {
			t.Fatal("broken hash chain")
		}
		last = e.Hash
		count++
	}
	if count != 3 {
		t.Fatalf("records=%d want 3", count)
	}
	if s.Status().TrackedJobs != 1 {
		t.Fatal("job index")
	}
}
