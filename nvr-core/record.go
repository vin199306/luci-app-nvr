package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// runRecord 录像主循环
func runRecord(ctx context.Context, cfg *Config) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// a. 检测磁盘是否挂载
		if !detectDiskMounted(cfg.DiskName) {
			log.Printf("磁盘 %s 未挂载，等待 10 秒重试", cfg.DiskName)
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
			continue
		}

		// b. 检查录像天数
		checkTotalDays(cfg.Directory, cfg.TotalDays)
		// c. 检查循环写入空间
		cleanupLoopWrite(cfg)

		// d. 对每路摄像头启动录像
		cams := cfg.BuildCameras()
		if len(cams) == 0 {
			log.Printf("无摄像头配置（nvr_sourcelist=%s）", cfg.Source)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(cfg.RecTime) * time.Second):
			}
			continue
		}

		recordCtx, recordCancel := context.WithTimeout(ctx, time.Duration(cfg.RecTime)*time.Second)
		var wg sync.WaitGroup
		for i, cam := range cams {
			wg.Add(1)
			go func(idx int, c Camera) {
				defer wg.Done()
				recordOne(recordCtx, cfg, idx, c)
			}(i+1, cam)
		}

		// 录像运行中，sleep rec_time - 3（与录像并发，对齐原有 shell 行为）
		sleepSec := cfg.RecTime - 3
		if sleepSec < 1 {
			sleepSec = 1
		}
		select {
		case <-ctx.Done():
			recordCancel()
			wg.Wait()
			return
		case <-time.After(time.Duration(sleepSec) * time.Second):
		}

		// 等待本批次录像结束
		wg.Wait()
		recordCancel()
	}
}

// recordOne 录制一路摄像头一个切片（rec_time 秒）
func recordOne(ctx context.Context, cfg *Config, channel int, cam Camera) {
	// 创建目录 {storage}/{YYYY-MM-DD}/{通道号}/
	day := time.Now().Format("2006-01-02")
	dir := filepath.Join(cfg.Directory, day, strconv.Itoa(channel))
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("通道 %d 创建目录 %s 失败: %v", channel, dir, err)
		return
	}

	// 文件名：FAT 分区用 - 分隔，其他用 : 分隔
	var name string
	if isFatPartition(cfg.Directory) {
		name = time.Now().Format("2006-01-02-15-04-05") + ".mp4"
	} else {
		name = time.Now().Format("2006-01-02-15:04:05") + ".mp4"
	}
	path := filepath.Join(dir, name)

	// 创建 MP4 muxer
	mux, err := NewMP4Muxer(path, cfg.EnableAudio == 1)
	if err != nil {
		log.Printf("通道 %d 创建 MP4 失败: %v", channel, err)
		return
	}

	// 启动 RTSP 拉流
	puller := NewRTSPPuller(cam.URL, cfg.EnableAudio == 1, mux)
	if err := puller.Start(); err != nil {
		log.Printf("通道 %d 拉流失败 [%s]: %v", channel, cam.URL, err)
		mux.Close()
		os.Remove(path) // 删除空文件
		return
	}

	log.Printf("通道 %d 开始录像: %s", channel, path)

	// 等待切片时长或流结束
	select {
	case <-ctx.Done():
	case <-puller.Wait():
		log.Printf("通道 %d 流提前结束", channel)
	}

	puller.Stop()
	if err := mux.Close(); err != nil {
		log.Printf("通道 %d 关闭 MP4 失败: %v", channel, err)
	}
	log.Printf("通道 %d 录像切片完成: %s", channel, path)
}
