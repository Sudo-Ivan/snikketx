package hostmetrics

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
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
	PortalRSS     *int64
	PortalCPU     *float64
	Load1         *float64
	Load5         *float64
	Load15        *float64
	MemTotal      *int64
	MemAvailable  *int64
	MemUsedRatio  *float64
	DiskTotal     *int64
	DiskUsed      *int64
	DiskAvail     *int64
	DiskUsedRatio *float64
	CPUPressure10 *float64
	MemPressure10 *float64
	IOPressure10  *float64
	Uptime        time.Duration
	Goroutines    int
	NumCPU        int
}

func Collect() Stats {
	s := Stats{
		Uptime:     time.Since(cpuEpoch),
		Goroutines: runtime.NumGoroutine(),
		NumCPU:     runtime.NumCPU(),
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

	if load1, load5, load15, ok := readLoadavg(); ok {
		s.Load1 = &load1
		s.Load5 = &load5
		s.Load15 = &load15
	}
	if total, avail, ok := readMeminfo(); ok {
		s.MemTotal = &total
		s.MemAvailable = &avail
		if total > 0 {
			used := float64(total-avail) / float64(total)
			if used < 0 {
				used = 0
			}
			s.MemUsedRatio = &used
		}
	}
	if total, used, avail, ok := readDisk("/"); ok {
		s.DiskTotal = &total
		s.DiskUsed = &used
		s.DiskAvail = &avail
		if total > 0 {
			ratio := float64(used) / float64(total)
			s.DiskUsedRatio = &ratio
		}
	}
	if v, ok := readPressureAvg10("cpu"); ok {
		s.CPUPressure10 = &v
	}
	if v, ok := readPressureAvg10("memory"); ok {
		s.MemPressure10 = &v
	}
	if v, ok := readPressureAvg10("io"); ok {
		s.IOPressure10 = &v
	}
	return s
}

func readRSS() int64 {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return int64(ms.Sys & 0x7fffffffffffffff) // #nosec G115 -- Sys is a size in bytes, clamp to int64 range
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

func readLoadavg() (load1, load5, load15 float64, ok bool) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0, false
	}
	var err1, err5, err15 error
	load1, err1 = strconv.ParseFloat(fields[0], 64)
	load5, err5 = strconv.ParseFloat(fields[1], 64)
	load15, err15 = strconv.ParseFloat(fields[2], 64)
	return load1, load5, load15, err1 == nil && err5 == nil && err15 == nil
}

func readLoad5() (float64, bool) {
	_, load5, _, ok := readLoadavg()
	return load5, ok
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

func readDisk(path string) (total, used, avail int64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, 0, false
	}
	bs := int64(st.Bsize) // #nosec G115 -- Bsize is a filesystem block size
	if bs <= 0 {
		return 0, 0, 0, false
	}
	total = int64(st.Blocks) * bs // #nosec G115 -- block counts fit int64 for host disks
	avail = int64(st.Bavail) * bs // #nosec G115 -- free blocks for unprivileged callers
	used = total - int64(st.Bfree)*bs // #nosec G115 -- free block counts fit int64 for host disks
	if used < 0 {
		used = 0
	}
	return total, used, avail, total > 0
}

func readPressureAvg10(kind string) (float64, bool) {
	switch kind {
	case "cpu", "memory", "io":
	default:
		return 0, false
	}
	data, err := os.ReadFile("/proc/pressure/" + kind) // #nosec G304 -- kind is allowlisted above
	if err != nil {
		return 0, false
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.HasPrefix(line, "some ") {
			continue
		}
		for field := range strings.FieldsSeq(line) {
			if after, ok := strings.CutPrefix(field, "avg10="); ok {
				v, err := strconv.ParseFloat(after, 64)
				return v, err == nil
			}
		}
	}
	return 0, false
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
