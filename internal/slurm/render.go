package slurm

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

func RenderPrometheus(w io.Writer, snap Snapshot) {
	groups := map[string][]Sample{}
	for _, s := range snap.Samples {
		groups[s.Name] = append(groups[s.Name], s)
	}
	names := make([]string, 0, len(groups))
	for n := range groups {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		g := groups[n]
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", n, g[0].Help, n, g[0].Type)
		for _, s := range g {
			fmt.Fprint(w, n)
			if len(s.Labels) > 0 {
				keys := make([]string, 0, len(s.Labels))
				for k := range s.Labels {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				fmt.Fprint(w, "{")
				for i, k := range keys {
					if i > 0 {
						fmt.Fprint(w, ",")
					}
					fmt.Fprintf(w, "%s=%s", k, strconv.Quote(s.Labels[k]))
				}
				fmt.Fprint(w, "}")
			}
			fmt.Fprintf(w, " %s\n", strconv.FormatFloat(s.Value, 'g', -1, 64))
		}
	}
	ok := 1
	if len(snap.Errors) > 0 {
		ok = 0
	}
	fmt.Fprintf(w, "# HELP slurm_exporter_up Whether every collector succeeded.\n# TYPE slurm_exporter_up gauge\nslurm_exporter_up %d\n", ok)
	fmt.Fprintf(w, "# HELP slurm_exporter_collection_duration_seconds Total collection duration.\n# TYPE slurm_exporter_collection_duration_seconds gauge\nslurm_exporter_collection_duration_seconds %g\n", snap.Duration)
	for _, e := range snap.Errors {
		fmt.Fprintf(w, "# ERROR %s\n", strings.ReplaceAll(e, "\n", " "))
	}
}
