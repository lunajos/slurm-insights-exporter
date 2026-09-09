package history

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/raging-racoons/slurm-insights-exporter/internal/slurm"
)

type state struct {
	Sequence uint64            `json:"sequence"`
	LastHash string            `json:"last_hash"`
	Jobs     map[string]string `json:"jobs"`
}
type Envelope struct {
	Sequence     uint64          `json:"sequence"`
	Timestamp    time.Time       `json:"timestamp"`
	Kind         string          `json:"kind"`
	PreviousHash string          `json:"previous_hash"`
	Data         json.RawMessage `json:"data"`
	Hash         string          `json:"hash"`
}
type observation struct {
	Duration float64        `json:"collection_duration_seconds"`
	Metrics  []slurm.Sample `json:"metrics"`
	Errors   []string       `json:"errors,omitempty"`
}
type Status struct {
	Directory   string    `json:"directory"`
	Sequence    uint64    `json:"sequence"`
	LastHash    string    `json:"last_hash"`
	TrackedJobs int       `json:"tracked_jobs"`
	LastWrite   time.Time `json:"last_write"`
}
type Store struct {
	mu        sync.Mutex
	dir       string
	state     state
	lastWrite time.Time
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, state: state{Jobs: map[string]string{}}}
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err == nil {
		if err = json.Unmarshal(b, &s.state); err != nil {
			return nil, fmt.Errorf("read history state: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if s.state.Jobs == nil {
		s.state.Jobs = map[string]string{}
	}
	return s, nil
}

func (s *Store) AppendSnapshot(snap slurm.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	obs := observation{snap.Duration, snap.Samples, snap.Errors}
	if err := s.append("observation", snap.CollectedAt, obs); err != nil {
		return err
	}
	jobs := append([]slurm.JobRecord(nil), snap.Jobs...)
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].JobID < jobs[j].JobID })
	for _, j := range jobs {
		b, _ := json.Marshal(j)
		sum := sha256.Sum256(b)
		fp := hex.EncodeToString(sum[:])
		key := j.Cluster + "/" + j.JobID
		if s.state.Jobs[key] == fp {
			continue
		}
		if err := s.append("job_state_change", snap.CollectedAt, j); err != nil {
			return err
		}
		s.state.Jobs[key] = fp
	}
	s.lastWrite = time.Now().UTC()
	return s.saveState()
}

func (s *Store) append(kind string, at time.Time, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	s.state.Sequence++
	e := Envelope{Sequence: s.state.Sequence, Timestamp: at.UTC(), Kind: kind, PreviousHash: s.state.LastHash, Data: raw}
	canonical, _ := json.Marshal(struct {
		Sequence           uint64    `json:"sequence"`
		Timestamp          time.Time `json:"timestamp"`
		Kind, PreviousHash string
		Data               json.RawMessage `json:"data"`
	}{e.Sequence, e.Timestamp, e.Kind, e.PreviousHash, e.Data})
	sum := sha256.Sum256(canonical)
	e.Hash = hex.EncodeToString(sum[:])
	path := filepath.Join(s.dir, at.UTC().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	err = json.NewEncoder(w).Encode(e)
	if err == nil {
		err = w.Flush()
	}
	if syncErr := f.Sync(); err == nil {
		err = syncErr
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		s.state.LastHash = e.Hash
	}
	return err
}

func (s *Store) saveState() error {
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.dir, ".state.tmp")
	if err = os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.dir, "state.json"))
}
func (s *Store) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{s.dir, s.state.Sequence, s.state.LastHash, len(s.state.Jobs), s.lastWrite}
}
