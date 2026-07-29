package main

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtpmpeg4audio"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/pion/rtp"

	gmcodec "github.com/yapingcat/gomedia/go-codec"
)

// FrameHandler 处理解码后的帧数据
// H264 为 AnnexB 格式（含 startcode），AAC 为 ADTS 格式（含 7 字节头）
type FrameHandler interface {
	OnH264(annexB []byte, pts time.Duration)
	OnAAC(adts []byte, pts time.Duration)
}

// RTSPPuller 拉取一路 RTSP 流并解码为 H264/AAC 帧
type RTSPPuller struct {
	url         string
	enableAudio bool
	handler     FrameHandler

	client    *gortsplib.Client
	h264Dec   *rtph264.Decoder
	aacDec    *rtpmpeg4audio.Decoder
	h264Media *description.Media
	aacMedia  *description.Media
	h264Fmt   *format.H264
	aacFmt    *format.MPEG4Audio

	ascBytes []byte // AAC AudioSpecificConfig，用于构造 ADTS 头

	startPTS int64
	hasStart bool
	done     chan struct{}
	started  bool
}

// NewRTSPPuller 创建 RTSP 拉流器
func NewRTSPPuller(url string, enableAudio bool, h FrameHandler) *RTSPPuller {
	return &RTSPPuller{
		url:         url,
		enableAudio: enableAudio,
		handler:     h,
		done:        make(chan struct{}),
	}
}

// Start 连接 RTSP 并开始拉流
func (p *RTSPPuller) Start() error {
	u, err := base.ParseURL(p.url)
	if err != nil {
		return fmt.Errorf("解析 URL 失败: %w", err)
	}

	p.client = &gortsplib.Client{
		Scheme: u.Scheme,
		Host:   u.Host,
	}
	if err := p.client.Start(); err != nil {
		return fmt.Errorf("连接 RTSP 失败: %w", err)
	}

	desc, _, err := p.client.Describe(u)
	if err != nil {
		p.client.Close()
		return fmt.Errorf("Describe 失败: %w", err)
	}

	// 查找 H264 媒体
	p.h264Media = desc.FindFormat(&p.h264Fmt)
	if p.h264Media == nil {
		p.client.Close()
		return errors.New("未找到 H264 媒体流")
	}
	p.h264Dec, err = p.h264Fmt.CreateDecoder()
	if err != nil {
		p.client.Close()
		return fmt.Errorf("创建 H264 解码器失败: %w", err)
	}

	// 查找 AAC 媒体（启用音频时）
	if p.enableAudio {
		p.aacMedia = desc.FindFormat(&p.aacFmt)
		if p.aacMedia != nil {
			if p.aacDec, err = p.aacFmt.CreateDecoder(); err != nil {
				log.Printf("创建 AAC 解码器失败: %v", err)
				p.aacMedia = nil
			} else if p.aacFmt.Config != nil {
				p.ascBytes, _ = p.aacFmt.Config.Marshal()
			}
		}
	}

	// Setup 所有媒体
	if err := p.client.SetupAll(desc.BaseURL, desc.Medias); err != nil {
		p.client.Close()
		return fmt.Errorf("Setup 失败: %w", err)
	}

	// 注册 RTP 包回调
	p.client.OnPacketRTP(p.h264Media, p.h264Fmt, p.onH264Packet)
	if p.aacMedia != nil {
		p.client.OnPacketRTP(p.aacMedia, p.aacFmt, p.onAACPacket)
	}

	if _, err := p.client.Play(nil); err != nil {
		p.client.Close()
		return fmt.Errorf("Play 失败: %w", err)
	}

	p.started = true
	go func() {
		defer close(p.done)
		if err := p.client.Wait(); err != nil {
			log.Printf("RTSP 拉流结束 [%s]: %v", p.url, err)
		}
	}()
	return nil
}

// Stop 停止拉流并等待结束
func (p *RTSPPuller) Stop() {
	if p.client != nil {
		p.client.Close()
	}
	if p.started {
		<-p.done
	}
}

// Wait 返回流结束信号通道
func (p *RTSPPuller) Wait() <-chan struct{} {
	return p.done
}

// onH264Packet 处理 H264 RTP 包
func (p *RTSPPuller) onH264Packet(pkt *rtp.Packet) {
	pts, ok := p.client.PacketPTS(p.h264Media, pkt)
	if !ok {
		return
	}
	au, err := p.h264Dec.Decode(pkt)
	if err != nil {
		if !errors.Is(err, rtph264.ErrMorePacketsNeeded) &&
			!errors.Is(err, rtph264.ErrNonStartingPacketAndNoPrevious) {
			log.Printf("H264 解码错误 [%s]: %v", p.url, err)
		}
		return
	}
	// 等待关键帧开始，避免文件开头花屏
	if !p.hasStart && !h264.IsRandomAccess(au) {
		return
	}
	annexB := toAnnexB(au)
	p.handler.OnH264(annexB, p.rel(pts))
}

// onAACPacket 处理 AAC RTP 包
func (p *RTSPPuller) onAACPacket(pkt *rtp.Packet) {
	pts, ok := p.client.PacketPTS(p.aacMedia, pkt)
	if !ok {
		return
	}
	aus, err := p.aacDec.Decode(pkt)
	if err != nil {
		log.Printf("AAC 解码错误 [%s]: %v", p.url, err)
		return
	}
	rel := p.rel(pts)
	for _, au := range aus {
		adts := p.makeADTS(au)
		p.handler.OnAAC(adts, rel)
	}
}

// rel 计算相对于起始的 PTS（输入为纳秒，返回 time.Duration）
func (p *RTSPPuller) rel(pts int64) time.Duration {
	if !p.hasStart {
		p.startPTS = pts
		p.hasStart = true
	}
	d := pts - p.startPTS
	if d < 0 {
		return 0
	}
	return time.Duration(d)
}

// makeADTS 为原始 AAC 帧添加 ADTS 头
func (p *RTSPPuller) makeADTS(rawAAC []byte) []byte {
	if len(p.ascBytes) == 0 {
		return rawAAC
	}
	hdr, err := gmcodec.ConvertASCToADTS(p.ascBytes, len(rawAAC)+7)
	if err != nil {
		return rawAAC
	}
	hdrBytes := hdr.Encode()
	return append(hdrBytes, rawAAC...)
}

// toAnnexB 将 access unit（无 startcode 的 NALU 列表）转为 AnnexB 格式
func toAnnexB(au [][]byte) []byte {
	var buf []byte
	for _, nalu := range au {
		buf = append(buf, 0, 0, 0, 1)
		buf = append(buf, nalu...)
	}
	return buf
}
