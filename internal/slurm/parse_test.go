package slurm

import "testing"

func TestDuration(t *testing.T) {
	for in, want := range map[string]float64{"01:02:03": 3723, "2-01:00:00": 176400, "05:30": 330, "UNLIMITED": 0} {
		if got := duration(in); got != want {
			t.Errorf("duration(%q)=%v want %v", in, got, want)
		}
	}
}
func TestBaseState(t *testing.T) {
	if got := baseState("idle+"); got != "IDLE" {
		t.Fatalf("got %q", got)
	}
}

func TestQuantities(t *testing.T) {
	if got := parseBytes("2G"); got != 2*1024*1024*1024 {
		t.Fatalf("bytes=%v", got)
	}
	if got := gpuCount("gpu:a100:2,gpu:h100:4"); got != 6 {
		t.Fatalf("gpus=%v", got)
	}
	if got := memoryAllocation("2Gc", 8, 2); got != 16*1024*1024*1024 {
		t.Fatalf("memory allocation=%v", got)
	}
}

func TestTRESNumeric(t *testing.T) {
	got := tresNumeric("cpu=64,mem=256G,billing=64,gres/gpu:a100=4(IDX:0-3)")
	if got["cpu"] != 64 || got["mem"] != 256*1024*1024*1024 || got["gres/gpu:a100"] != 4 {
		t.Fatalf("TRES=%v", got)
	}
}
