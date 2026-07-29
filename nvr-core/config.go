package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Config 保存所有 uci 配置项
type Config struct {
	Source      string // nvr_sourcelist
	Directory   string // storage_directory（已去尾斜杠）
	DiskName    string // disk_name
	RecTime     int    // rec_time（秒）
	StorageSize int    // storage_size（MB）
	TotalDays   int    // total_days
	LoopWrite   int    // loop_write 1/0
	FullDisk    int    // fulldisk 1/0
	DiskUsage   int    // disk_usage（%）
	EnableAudio int    // enable_audio 1/0
	Enabled     int    // enabled 1/0

	// hikvision
	HikUser       string
	HikPass       string
	HikList       string // one-by-one|batch-add|none
	HikPush       string
	HikBatchStart string
	HikBatchEnd   string

	// tplink
	TplinkUser       string
	TplinkPass       string
	TplinkList       string
	TplinkPush       string
	TplinkBatchStart string
	TplinkBatchEnd   string

	// 推流
	RtmpPush      string // rtmp-url 模式的 URL 列表
	MultiPush     string // multiple-types 模式的 URL 列表
	DoPush        int    // do_push 1/0
	RtmpServerApp string // rtmp_server_app，如 rtmp://ip:1935/app
}

// uciGet 通过 shell out 读取 uci 配置
func uciGet(key string) string {
	out, err := exec.Command("uci", "get", "nvr.config."+key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// uciGetInt 读取整型配置，失败返回默认值
func uciGetInt(key string, def int) int {
	s := uciGet(key)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

// LoadConfig 从 uci 加载全部配置
func LoadConfig() *Config {
	c := &Config{}
	c.Source = uciGet("nvr_sourcelist")
	c.Directory = strings.TrimRight(uciGet("storage_directory"), "/")
	c.DiskName = uciGet("disk_name")
	c.RecTime = uciGetInt("rec_time", 600)
	c.StorageSize = uciGetInt("storage_size", 1000)
	c.TotalDays = uciGetInt("total_days", 30)
	c.LoopWrite = uciGetInt("loop_write", 0)
	c.FullDisk = uciGetInt("fulldisk", 0)
	c.DiskUsage = uciGetInt("disk_usage", 85)
	c.EnableAudio = uciGetInt("enable_audio", 0)
	c.Enabled = uciGetInt("enabled", 0)

	c.HikUser = uciGet("hik_user")
	c.HikPass = uciGet("hik_pass")
	c.HikList = uciGet("hik_list")
	c.HikPush = uciGet("hikpush")
	c.HikBatchStart = uciGet("hik_batch_start")
	c.HikBatchEnd = uciGet("hik_batch_end")

	c.TplinkUser = uciGet("tplink_user")
	c.TplinkPass = uciGet("tplink_pass")
	c.TplinkList = uciGet("tplink_list")
	c.TplinkPush = uciGet("tplinkpush")
	c.TplinkBatchStart = uciGet("tplink_batch_start")
	c.TplinkBatchEnd = uciGet("tplink_batch_end")

	c.RtmpPush = uciGet("rtmppush")
	c.MultiPush = uciGet("multipush")
	c.DoPush = uciGetInt("do_push", 0)
	c.RtmpServerApp = uciGet("rtmp_server_app")
	return c
}

// Camera 表示一路摄像头
type Camera struct {
	URL string // 输入流 URL（rtsp 或 rtmp）
	IP  string // IP 地址，用于录像通道命名和推流路径
}

// BuildCameras 根据配置构造摄像头列表
func (c *Config) BuildCameras() []Camera {
	var cams []Camera
	switch c.Source {
	case "hikvision":
		for _, ip := range c.hikIPs() {
			cams = append(cams, Camera{
				URL: fmt.Sprintf("rtsp://%s:%s@%s:554/h264/ch1/main/av_stream", c.HikUser, c.HikPass, ip),
				IP:  ip,
			})
		}
	case "tplink":
		for _, ip := range c.tplinkIPs() {
			cams = append(cams, Camera{
				URL: fmt.Sprintf("rtsp://%s:%s@%s:554/stream1", c.TplinkUser, c.TplinkPass, ip),
				IP:  ip,
			})
		}
	case "rtmp-url":
		for _, u := range fields(c.RtmpPush) {
			cams = append(cams, Camera{URL: u, IP: u})
		}
	case "multiple-types":
		for _, u := range fields(c.MultiPush) {
			cams = append(cams, Camera{URL: u, IP: u})
		}
	}
	return cams
}

func (c *Config) hikIPs() []string {
	if c.HikList == "batch-add" {
		return genIPRange(c.HikBatchStart, c.HikBatchEnd)
	}
	return fields(c.HikPush)
}

func (c *Config) tplinkIPs() []string {
	if c.TplinkList == "batch-add" {
		return genIPRange(c.TplinkBatchStart, c.TplinkBatchEnd)
	}
	return fields(c.TplinkPush)
}

// fields 按空白分隔字符串
func fields(s string) []string {
	return strings.Fields(s)
}

// genIPRange 生成 IP 范围（含起止），如 192.168.1.64 到 192.168.1.200
func genIPRange(start, end string) []string {
	if start == "" || end == "" {
		return nil
	}
	startParts := strings.Split(start, ".")
	if len(startParts) != 4 {
		return nil
	}
	endParts := strings.Split(end, ".")
	if len(endParts) != 4 {
		return nil
	}
	prefix := startParts[0] + "." + startParts[1] + "." + startParts[2] + "."
	startNum, err := strconv.Atoi(startParts[3])
	if err != nil {
		return nil
	}
	endNum, err := strconv.Atoi(endParts[3])
	if err != nil {
		return nil
	}
	if endNum < startNum {
		return nil
	}
	ips := make([]string, 0, endNum-startNum+1)
	for i := startNum; i <= endNum; i++ {
		ips = append(ips, prefix+strconv.Itoa(i))
	}
	return ips
}
