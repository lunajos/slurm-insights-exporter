package slurm

import "time"

type Sample struct {
	Name   string            `json:"name"`
	Help   string            `json:"-"`
	Type   string            `json:"-"`
	Labels map[string]string `json:"labels,omitempty"`
	Value  float64           `json:"value"`
}

type JobRecord struct {
	Cluster               string  `json:"cluster"`
	JobID                 string  `json:"job_id"`
	User                  string  `json:"user,omitempty"`
	Account               string  `json:"account"`
	Partition             string  `json:"partition"`
	QOS                   string  `json:"qos"`
	State                 string  `json:"state"`
	AllocCPUs             float64 `json:"allocated_cpus"`
	ElapsedSeconds        float64 `json:"elapsed_seconds"`
	CPUSeconds            float64 `json:"cpu_seconds"`
	WaitSeconds           float64 `json:"wait_seconds"`
	RequestedMemoryBytes  float64 `json:"requested_memory_bytes"`
	MaxRSSBytes           float64 `json:"max_rss_bytes"`
	EnergyJoules          float64 `json:"energy_joules"`
	AllocatedTRES         string  `json:"allocated_tres,omitempty"`
	RequestedTRES         string  `json:"requested_tres,omitempty"`
	CPUEfficiencyRatio    float64 `json:"cpu_efficiency_ratio"`
	MemoryEfficiencyRatio float64 `json:"memory_efficiency_ratio"`
}

type Snapshot struct {
	CollectedAt time.Time   `json:"collected_at"`
	Duration    float64     `json:"collection_duration_seconds"`
	Samples     []Sample    `json:"metrics"`
	Jobs        []JobRecord `json:"jobs,omitempty"`
	Errors      []string    `json:"errors,omitempty"`
}
