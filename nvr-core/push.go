package main

import (
	"context"
	"log"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	gmcodec "github.com/yapingcat/gomedia/go-codec"
	"github.com/yapingcat/gomedia/go-rtmp"
)

// runPush RTMP 推流主入口
func runPush(ctx context.Context, cfg *Config) {
	cams := cfg.BuildCameras()
	if len(cams) == 0 {
		log.Printf("无摄像头配置（nvr_sourcelist=%s），推流退出", cfg.Source)
		return
	}

	var wg sync.WaitGroup
	for _, cam := range cams {
		wg.Add(1)
		go func(c Camera) {
			defer wg.Done()
			pushOne(ctx, cfg, c)
		}(cam)
	}
	wg.Wait()
}

// pushTarget 构造推流目标 URL：{rtmp_server_app}/{ip}
// 对于 rtmp-url/multiple-types 模式，输入是完整 URL，取其 host 作为流名
func pushTarget(cfg *Config, cam Camera) string {
	ip := cam.IP
	if strings.HasPrefix(ip, "rtmp://") || strings.HasPrefix(ip, "rtsp://") {
		if u, err := url.Parse(ip); err == nil {
			ip = u.Hostname()
		}
	}
	return strings.TrimRight(cfg.RtmpServerApp, "/") + "/" + ip
}

// pushOne 拉取一路 RTSP 并推送到 RTMP
func pushOne(ctx context.Context, cfg *Config, cam Camera) {
	target := pushTarget(cfg, cam)
	log.Printf("开始推流: %s -> %s", cam.URL, target)

	// 解析 RTMP 地址并建立 TCP 连接
	u, err := url.Parse(target)
	if err != nil {
		log.Printf("解析 RTMP URL 失败: %v", err)
		return
	}
	host := u.Host
	if u.Port() == "" {
		host += ":1935"
	}
	conn, err := net.Dial("tcp", host)
	if err != nil {
		log.Printf("连接 RTMP 服务器失败: %v", err)
		return
	}
	defer conn.Close()

	// 创建 RTMP 客户端
	ready := make(chan struct{})
	cli := rtmp.NewRtmpClient(rtmp.WithComplexHandshake(), rtmp.WithEnablePublish())
	cli.OnStateChange(func(newState rtmp.RtmpState) {
		if newState == rtmp.STATE_RTMP_PUBLISH_START {
			close(ready)
		}
	})
	cli.SetOutput(func(data []byte) error {
		_, err := conn.Write(data)
		return err
	})
	go cli.Start(target)

	// 读取 RTMP 响应数据
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			cli.Input(buf[:n])
		}
	}()

	// 等待推流就绪
	select {
	case <-ready:
	case <-ctx.Done():
		return
	case <-time.After(10 * time.Second):
		log.Printf("RTMP 推流就绪超时: %s", target)
		return
	}

	log.Printf("RTMP 推流就绪: %s", target)

	// 拉取 RTSP 流并转发到 RTMP
	handler := &rtmpForwarder{cli: cli}
	puller := NewRTSPPuller(cam.URL, cfg.EnableAudio == 1, handler)
	if err := puller.Start(); err != nil {
		log.Printf("拉流失败 [%s]: %v", cam.URL, err)
		return
	}

	// 等待结束
	select {
	case <-ctx.Done():
	case <-puller.Wait():
		log.Printf("推流源结束: %s", cam.URL)
	}
	puller.Stop()
	log.Printf("推流停止: %s", target)
}

// rtmpForwarder 实现 FrameHandler，将帧转发到 RTMP
type rtmpForwarder struct {
	cli *rtmp.RtmpClient
}

func (f *rtmpForwarder) OnH264(annexB []byte, pts time.Duration) {
	ms := uint32(pts.Milliseconds())
	if err := f.cli.WriteVideo(gmcodec.CODECID_VIDEO_H264, annexB, ms, ms); err != nil {
		log.Printf("RTMP 写视频失败: %v", err)
	}
}

func (f *rtmpForwarder) OnAAC(adts []byte, pts time.Duration) {
	ms := uint32(pts.Milliseconds())
	if err := f.cli.WriteAudio(gmcodec.CODECID_AUDIO_AAC, adts, ms, ms); err != nil {
		log.Printf("RTMP 写音频失败: %v", err)
	}
}
