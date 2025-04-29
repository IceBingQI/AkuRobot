package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type SystemInfo struct {
	CPU struct {
		Usage     float64 `json:"usage"`
		NumCPU    int     `json:"num_cpu"`     // CPU核心数
		GoMaxProc int     `json:"go_max_proc"` // Go程序可用的最大CPU数
	} `json:"cpu"`
	Memory struct {
		Total uint64 `json:"total"`
		Used  uint64 `json:"used"`
	} `json:"memory"`
	Battery struct {
		Status   string `json:"status"`   // 充电状态
		Capacity int    `json:"capacity"` // 电量百分比
	} `json:"battery"`
	System struct {
		OS           string    `json:"os"`            // 操作系统
		Architecture string    `json:"architecture"`  // 系统架构
		NumGoroutine int       `json:"num_goroutine"` // 当前goroutine数量
		GoVersion    string    `json:"go_version"`    // Go版本
		StartTime    time.Time `json:"start_time"`    // 程序启动时间
		WorkDir      string    `json:"work_dir"`      // 工作目录
		Hostname     string    `json:"hostname"`      // 主机名
	} `json:"system"`
}

var (
	startTime    = time.Now()
	lastCPUUsage float64
	lastCPUCheck time.Time
)

// getCPUUsage 获取CPU使用率
func getCPUUsage() float64 {
	// 读取 /proc/stat 文件获取CPU信息
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return lastCPUUsage // 如果读取失败，返回上次的值
	}

	var user, nice, system, idle, iowait, irq, softirq, steal uint64
	_, err = fmt.Sscanf(string(data), "cpu %d %d %d %d %d %d %d %d",
		&user, &nice, &system, &idle, &iowait, &irq, &softirq, &steal)
	if err != nil {
		return lastCPUUsage
	}

	idle_total := idle + iowait
	non_idle := user + nice + system + irq + softirq + steal
	total := idle_total + non_idle

	now := time.Now()
	if !lastCPUCheck.IsZero() {
		// 计算时间差
		timeDiff := now.Sub(lastCPUCheck).Seconds()
		if timeDiff > 0 {
			cpuUsage := (float64(total-lastTotal) - float64(idle_total-lastIdleTotal)) / float64(total-lastTotal) * 100
			lastCPUUsage = cpuUsage
		}
	}

	// 保存当前值用于下次计算
	lastTotal = total
	lastIdleTotal = idle_total
	lastCPUCheck = now

	return lastCPUUsage
}

var (
	lastTotal     uint64
	lastIdleTotal uint64
)

// getBatteryStatus 获取电池状态
func getBatteryStatus() string {
	data, err := os.ReadFile("/sys/class/power_supply/axp20x-battery/status")
	if err != nil {
		log.Printf("读取电池状态失败: %v", err)
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

// getBatteryCapacity 获取电池电量
func getBatteryCapacity() int {
	data, err := os.ReadFile("/sys/class/power_supply/axp20x-battery/capacity")
	if err != nil {
		log.Printf("读取电池电量失败: %v", err)
		return -1
	}
	capacity, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		log.Printf("解析电池电量失败: %v", err)
		return -1
	}
	return capacity
}

// HandleSystemInfo 处理系统信息请求
func HandleSystemInfo(w http.ResponseWriter, r *http.Request) {
	info := SystemInfo{}

	// 获取CPU信息
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	// 获取系统内存信息
	memInfo, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		log.Printf("读取内存信息失败: %v", err)
		// 如果读取失败，使用Go的内存统计作为备选
		info.Memory.Total = stats.Sys
		info.Memory.Used = stats.Alloc
	} else {
		// 解析内存信息
		lines := strings.Split(string(memInfo), "\n")
		var totalMem, freeMem, buffers, cached uint64
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			value, _ := strconv.ParseUint(fields[1], 10, 64)
			switch fields[0] {
			case "MemTotal:":
				totalMem = value * 1024 // 转换为字节
			case "MemFree:":
				freeMem = value * 1024
			case "Buffers:":
				buffers = value * 1024
			case "Cached:":
				cached = value * 1024
			}
		}
		// 计算已使用内存 = 总内存 - 空闲内存 - 缓存 - 缓冲区
		info.Memory.Total = totalMem
		info.Memory.Used = totalMem - freeMem - buffers - cached
	}

	// CPU相关信息
	info.CPU.Usage = getCPUUsage()
	info.CPU.NumCPU = runtime.NumCPU()
	info.CPU.GoMaxProc = runtime.GOMAXPROCS(0)

	// 电池信息
	info.Battery.Status = getBatteryStatus()
	info.Battery.Capacity = getBatteryCapacity()

	// 系统信息
	info.System.OS = runtime.GOOS
	info.System.Architecture = runtime.GOARCH
	info.System.NumGoroutine = runtime.NumGoroutine()
	info.System.GoVersion = runtime.Version()
	info.System.StartTime = startTime

	// 获取工作目录
	if workDir, err := os.Getwd(); err == nil {
		info.System.WorkDir = filepath.Clean(workDir)
	}

	// 获取主机名
	if hostname, err := os.Hostname(); err == nil {
		info.System.Hostname = hostname
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// HandleSyncTime 处理时间同步请求
func HandleSyncTime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 使用ntpdate同步时间
	cmd := exec.Command("ntpdate", "-u", "ntp1.aliyun.com")
	output, err := cmd.CombinedOutput()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{
			"message": fmt.Sprintf("同步时间失败: %v, 输出: %s", err, string(output)),
		})
		return
	}
	// 更新启动时间
	startTime = time.Now()
	json.NewEncoder(w).Encode(map[string]string{
		"message": "时间同步成功",
	})
}
