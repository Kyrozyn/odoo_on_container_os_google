package system

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/runodoo/runodoo/internal/models"
)

// CollectSystemInfo inspects host CPU, RAM, Disk, and OS (including Google COS detection).
func CollectSystemInfo(dataDir, panelPort string, dockerConnected bool, dockerVersion string) models.SystemInfo {
	hostname, _ := os.Hostname()
	osPretty, isCOS := detectOS()
	totalRAM, availRAM := detectRAM()
	diskTotal, diskFree := detectDisk(dataDir)

	return models.SystemInfo{
		Hostname:        hostname,
		OSPretty:        osPretty,
		IsCOS:           isCOS,
		Arch:            runtime.GOARCH,
		CPUCores:        runtime.NumCPU(),
		TotalRAMMB:      totalRAM,
		AvailableRAMMB:  availRAM,
		DiskTotalGB:     diskTotal,
		DiskFreeGB:      diskFree,
		DockerConnected: dockerConnected,
		DockerVersion:   dockerVersion,
		PanelPort:       panelPort,
	}
}

// RecommendedOdooResources computes smart defaults matching install-odoo.sh for small VPS/e2-micro.
func RecommendedOdooResources(totalRAMMB int64, cpuCores int) models.ResourceConfig {
	if totalRAMMB > 0 && totalRAMMB <= 1500 {
		return models.ResourceConfig{
			Workers:         0,
			LimitMemorySoft: 671088640,  // 640MB
			LimitMemoryHard: 805306368,  // 768MB
			LimitTimeCPU:    36000,
			LimitTimeReal:   36000,
			MaxCronThreads:  1,
		}
	}
	workers := (cpuCores * 2) + 1
	if workers > 4 {
		workers = 4
	}
	if workers < 2 {
		workers = 2
	}
	return models.ResourceConfig{
		Workers:         workers,
		LimitMemorySoft: 805306368,  // 768MB
		LimitMemoryHard: 1073741824, // 1GB
		LimitTimeCPU:    36000,
		LimitTimeReal:   36000,
		MaxCronThreads:  1,
	}
}

// RecommendedPostgresTuning computes PostgreSQL flags for host RAM.
func RecommendedPostgresTuning(totalRAMMB int64) []string {
	if totalRAMMB <= 0 {
		totalRAMMB = 1024
	}
	sharedBuf := totalRAMMB / 4
	if sharedBuf < 128 {
		sharedBuf = 128
	}
	effCache := totalRAMMB / 2
	if effCache < 256 {
		effCache = 256
	}
	maintMem := totalRAMMB / 16
	if maintMem < 32 {
		maintMem = 32
	}
	return []string{
		"postgres",
		"-c", "shared_buffers=" + strconv.FormatInt(sharedBuf, 10) + "MB",
		"-c", "effective_cache_size=" + strconv.FormatInt(effCache, 10) + "MB",
		"-c", "maintenance_work_mem=" + strconv.FormatInt(maintMem, 10) + "MB",
		"-c", "work_mem=4MB",
		"-c", "max_connections=100",
	}
}

func detectOS() (string, bool) {
	if runtime.GOOS != "linux" {
		return runtime.GOOS + " (" + runtime.GOARCH + ")", false
	}
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return "Linux", false
	}
	defer f.Close()

	var pretty, id string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			pretty = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		} else if strings.HasPrefix(line, "ID=") {
			id = strings.Trim(strings.TrimPrefix(line, "ID="), `"`)
		}
	}
	if pretty == "" {
		pretty = "Linux"
	}
	return pretty, strings.EqualFold(id, "cos")
}

func detectRAM() (totalMB int64, availMB int64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		valKB, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			totalMB = valKB / 1024
		case "MemAvailable:":
			availMB = valKB / 1024
		}
	}
	return totalMB, availMB
}

func detectDisk(path string) (totalGB float64, freeGB float64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0
	}
	total := float64(stat.Blocks) * float64(stat.Bsize) / (1024 * 1024 * 1024)
	free := float64(stat.Bavail) * float64(stat.Bsize) / (1024 * 1024 * 1024)
	return float64(int(total*10)) / 10, float64(int(free*10)) / 10
}
