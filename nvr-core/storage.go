package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// detectDiskMounted 检测磁盘是否挂载（df -hT | grep diskName）
func detectDiskMounted(diskName string) bool {
	if diskName == "" {
		return true
	}
	out, err := exec.Command("df", "-hT").Output()
	if err != nil {
		return true // 无法判断时认为已挂载
	}
	return strings.Contains(string(out), diskName)
}

// isFatPartition 检测存储目录所在分区是否为 fat 类型
func isFatPartition(dir string) bool {
	out, err := exec.Command("df", "-T", dir).Output()
	if err != nil {
		return false
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) < 2 {
		return false
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 2 {
		return false
	}
	return strings.Contains(strings.ToLower(fields[1]), "fat")
}

// listDayDirs 列出存储目录下的日期目录（按名排序，最早在前）
func listDayDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	return dirs
}

// listChannelDirs 列出某日期目录下的通道目录（按名排序）
func listChannelDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	return dirs
}

// listFiles 列出目录下的文件（按名排序，最早在前）
func listFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files
}

// checkTotalDays 检查录像天数，目录数超过 totalDays 则删除最早的日期目录
func checkTotalDays(dir string, totalDays int) {
	if totalDays <= 0 {
		return
	}
	dirs := listDayDirs(dir)
	for len(dirs) > totalDays {
		oldest := filepath.Join(dir, dirs[0])
		if err := os.RemoveAll(oldest); err != nil {
			log.Printf("删除过期目录 %s 失败: %v", oldest, err)
			return
		}
		log.Printf("已删除过期录像目录: %s", oldest)
		dirs = dirs[1:]
	}
}

// diskUsagePercent 获取磁盘使用率百分比
func diskUsagePercent(diskName string) int {
	out, err := exec.Command("df", "-h").Output()
	if err != nil {
		return 0
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if diskName != "" && !strings.Contains(line, diskName) {
			continue
		}
		fields := strings.Fields(line)
		for _, f := range fields {
			if strings.HasSuffix(f, "%") {
				n, err := strconv.Atoi(strings.TrimSuffix(f, "%"))
				if err == nil {
					return n
				}
			}
		}
	}
	return 0
}

// estimatedUsageMB 估算存储目录已用空间（MB）
func estimatedUsageMB(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total / (1024 * 1024)
}

// needCleanup 判断是否需要清理空间
func needCleanup(cfg *Config) bool {
	if cfg.FullDisk == 1 {
		return diskUsagePercent(cfg.DiskName) >= cfg.DiskUsage
	}
	return estimatedUsageMB(cfg.Directory) >= int64(cfg.StorageSize)
}

// cleanupLoopWrite 循环写入空间清理：删除最早的录像文件直到空间足够
func cleanupLoopWrite(cfg *Config) {
	if cfg.LoopWrite != 1 {
		return
	}
	for needCleanup(cfg) {
		dayDirs := listDayDirs(cfg.Directory)
		if len(dayDirs) == 0 {
			return
		}
		dayPath := filepath.Join(cfg.Directory, dayDirs[0])
		chDirs := listChannelDirs(dayPath)
		if len(chDirs) == 0 {
			// 日期目录下无通道，删除空日期目录
			os.RemoveAll(dayPath)
			continue
		}
		chPath := filepath.Join(dayPath, chDirs[0])
		files := listFiles(chPath)
		if len(files) == 0 {
			// 通道目录为空，删除
			os.RemoveAll(chPath)
			continue
		}
		oldest := filepath.Join(chPath, files[0])
		if err := os.Remove(oldest); err != nil {
			log.Printf("删除录像文件 %s 失败: %v", oldest, err)
			return
		}
		// 通道目录空了则删除
		if len(listFiles(chPath)) == 0 {
			os.RemoveAll(chPath)
		}
		// 日期目录空了则删除
		if len(listChannelDirs(dayPath)) == 0 {
			os.RemoveAll(dayPath)
		}
	}
}
