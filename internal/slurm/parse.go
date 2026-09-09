package slurm

import (
	"regexp"
	"strconv"
	"strings"
)

func lines(b []byte) [][]string {
	var out [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, strings.Split(line, "|"))
		}
	}
	return out
}

func number(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "N/A" || s == "Unknown" || s == "UNLIMITED" {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func duration(s string) float64 {
	s = strings.TrimSpace(strings.SplitN(s, ".", 2)[0])
	if s == "" || s == "Unknown" || s == "UNLIMITED" || s == "Partition_Limit" {
		return 0
	}
	days := 0.0
	if p := strings.IndexByte(s, '-'); p >= 0 {
		days = number(s[:p])
		s = s[p+1:]
	}
	p := strings.Split(s, ":")
	var h, m, sec float64
	switch len(p) {
	case 3:
		h, m, sec = number(p[0]), number(p[1]), number(p[2])
	case 2:
		m, sec = number(p[0]), number(p[1])
	case 1:
		sec = number(p[0])
	}
	return days*86400 + h*3600 + m*60 + sec
}

func baseState(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, "+")
	if i := strings.IndexAny(s, " "); i >= 0 {
		s = s[:i]
	}
	return s
}

func clean(s string) string { return strings.TrimSpace(strings.TrimSuffix(s, "*")) }

var quantityRE = regexp.MustCompile(`(?i)^([0-9.]+)([KMGTPE]?)([cn]?)$`)

func parseBytes(s string) float64 {
	m := quantityRE.FindStringSubmatch(strings.TrimSpace(s))
	if len(m) != 4 {
		return number(s)
	}
	v := number(m[1])
	powers := map[string]int{"": 0, "K": 1, "M": 2, "G": 3, "T": 4, "P": 5, "E": 6}
	for i := 0; i < powers[strings.ToUpper(m[2])]; i++ {
		v *= 1024
	}
	return v
}

func memoryAllocation(s string, cpus, nodes float64) float64 {
	m := quantityRE.FindStringSubmatch(strings.TrimSpace(s))
	v := parseBytes(s)
	if len(m) != 4 {
		return v
	}
	switch strings.ToLower(m[3]) {
	case "c":
		return v * cpus
	case "n":
		return v * nodes
	default:
		return v
	}
}

var gpuRE = regexp.MustCompile(`(?i)(?:^|,)gpu(?::[^:,()]+)?(?::|=)([0-9]+)`)

func gpuCount(s string) float64 {
	var n float64
	for _, m := range gpuRE.FindAllStringSubmatch(s, -1) {
		n += number(m[1])
	}
	return n
}
