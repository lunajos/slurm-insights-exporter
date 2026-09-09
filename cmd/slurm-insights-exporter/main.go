package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/raging-racoons/slurm-insights-exporter/internal/history"
	"github.com/raging-racoons/slurm-insights-exporter/internal/slurm"
)

var version = "dev"

func main() {
	listen := flag.String("web.listen-address", env("LISTEN_ADDRESS", ":9341"), "HTTP listen address")
	cluster := flag.String("slurm.cluster", env("SLURM_CLUSTER", "default"), "Cluster label")
	timeout := flag.Duration("slurm.timeout", durationEnv("SLURM_TIMEOUT", 15*time.Second), "Timeout per Slurm command")
	window := flag.Duration("accounting.window", durationEnv("ACCOUNTING_WINDOW", 24*time.Hour), "sacct lookback window")
	ttl := flag.Duration("cache.ttl", durationEnv("CACHE_TTL", 30*time.Second), "Snapshot cache lifetime")
	slow := flag.Duration("cache.slow-ttl", durationEnv("SLOW_CACHE_TTL", 5*time.Minute), "Accounting and administrative collector cache lifetime")
	users := flag.Bool("labels.users", false, "Include user names (high cardinality/PII)")
	jobs := flag.Bool("metrics.per-job", false, "Expose per-running-job metrics (high cardinality)")
	historyDir := flag.String("history.dir", env("HISTORY_DIR", ""), "Append-only history directory; empty disables local archival")
	historyInterval := flag.Duration("history.interval", durationEnv("HISTORY_INTERVAL", 5*time.Minute), "History observation interval")
	flag.Parse()
	cfg := slurm.Config{Cluster: *cluster, Timeout: *timeout, AccountingWindow: *window, SlowRefresh: *slow, IncludeUsers: *users, IncludeJobs: *jobs}
	if err := cfg.Validate(); err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(2)
	}
	collector := slurm.NewCollector(slurm.ExecRunner{}, cfg)
	cache := slurm.NewCache(*ttl, collector.Collect)
	var archive *history.Store
	if *historyDir != "" {
		var err error
		archive, err = history.Open(*historyDir)
		if err != nil {
			slog.Error("open history archive", "error", err)
			os.Exit(1)
		}
		go func() {
			write := func() {
				if err := archive.AppendSnapshot(cache.Get(context.Background())); err != nil {
					slog.Error("archive snapshot", "error", err)
				}
			}
			write()
			ticker := time.NewTicker(*historyInterval)
			defer ticker.Stop()
			for range ticker.C {
				write()
			}
		}()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		slurm.RenderPrometheus(w, cache.Get(r.Context()))
	})
	mux.HandleFunc("/api/v1/snapshot", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cache.Get(r.Context()))
	})
	mux.HandleFunc("/api/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		snap := cache.Get(r.Context())
		limit := 1000
		if n, e := strconv.Atoi(r.URL.Query().Get("limit")); e == nil && n > 0 {
			limit = n
		}
		if limit > 10000 {
			limit = 10000
		}
		state := strings.ToUpper(r.URL.Query().Get("state"))
		account := r.URL.Query().Get("account")
		jobs := make([]slurm.JobRecord, 0)
		for _, j := range snap.Jobs {
			if state != "" && j.State != state {
				continue
			}
			if account != "" && j.Account != account {
				continue
			}
			jobs = append(jobs, j)
			if len(jobs) >= limit {
				break
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"collected_at": snap.CollectedAt, "count": len(jobs), "jobs": jobs, "errors": snap.Errors})
	})
	mux.HandleFunc("/api/v1/history/status", func(w http.ResponseWriter, r *http.Request) {
		if archive == nil {
			http.Error(w, "history is disabled", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(archive.Status())
	})
	mux.HandleFunc("/-/healthy", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "slurm-insights-exporter %s\n/metrics\n/api/v1/snapshot\n/api/v1/jobs\n/api/v1/history/status\n/-/healthy\n", version)
	})
	s := http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	slog.Info("starting exporter", "address", *listen, "cluster", *cluster, "version", version)
	if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func durationEnv(k string, d time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	x, e := time.ParseDuration(v)
	if e != nil {
		return d
	}
	return x
}
