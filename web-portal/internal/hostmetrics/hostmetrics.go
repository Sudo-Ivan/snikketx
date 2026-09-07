package hostmetrics

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	cpuEpoch      = time.Now()
	processCPUSec float64
)

func init() {
	processCPUSec = cpuSeconds()
}

type Stats struct {
	PortalRSS    *int64
	PortalCPU    *float64
	Load5        *float64
	MemTotal     *int64
	MemAvailable *int64
	Uptime       time.Duration
	Goroutines   int
}

func Collect() Stats {
	s := Stats{
		Uptime:     time.Since(cpuEpoch),
		Goroutines: runtime.NumGoroutine(),
	}

	if rss := readRSS(); rss > 0 {
		s.PortalRSS = &rss
	}

	elapsed := time.Since(cpuEpoch).Seconds()
	if elapsed > 0 {
		cpu := (cpuSeconds() - processCPUSec) / elapsed
		if cpu < 0 {
			cpu = 0
		}
		s.PortalCPU = &cpu
	}

	if load, ok := readLoad5(); ok {
		s.Load5 = &load
	}
	if total, avail, ok := readMeminfo(); ok {
		s.MemTotal = &total
		s.MemAvailable = &avail
	}
	return s
}

func readRSS() int64 {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return int64(ms.Sys)
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return pages * int64(os.Getpagesize())
}

func readLoad5() (float64, bool) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0, false
	}
	v, err := strconv.ParseFloat(fields[1], 64)
	return v, err == nil
}

func readMeminfo() (total, avail int64, ok bool) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		n *= 1024
		switch fields[0] {
		case "MemTotal:":
			total = n
		case "MemAvailable:":
			avail = n
		}
	}
	return total, avail, total > 0
}

func cpuSeconds() float64 {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 15 {
		return 0
	}
	utime, _ := strconv.ParseFloat(fields[13], 64)
	stime, _ := strconv.ParseFloat(fields[14], 64)
	hz := float64(100)
	return (utime + stime) / hz
}
