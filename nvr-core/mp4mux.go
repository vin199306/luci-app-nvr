package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/yapingcat/gomedia/go-mp4"
)

// MP4Muxer 封装 gomedia 的 MP4 muxer，实现 FrameHandler 接口
type MP4Muxer struct {
	file  *os.File
	muxer *mp4.Movmuxer
	vtid  uint32
	atid  uint32
}

// NewMP4Muxer 创建 MP4 文件 muxer
// enableAudio 为 true 时同时添加 AAC 音轨
func NewMP4Muxer(path string, enableAudio bool) (*MP4Muxer, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("创建文件失败: %w", err)
	}
	m, err := mp4.CreateMp4Muxer(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("创建 muxer 失败: %w", err)
	}
	mux := &MP4Muxer{file: f, muxer: m}
	mux.vtid = m.AddVideoTrack(mp4.MP4_CODEC_H264)
	if enableAudio {
		mux.atid = m.AddAudioTrack(mp4.MP4_CODEC_AAC)
	}
	return mux, nil
}

// OnH264 写入一帧 H264（AnnexB 格式），pts 为相对起始的时间戳
func (m *MP4Muxer) OnH264(annexB []byte, pts time.Duration) {
	ms := uint64(pts.Milliseconds())
	if err := m.muxer.Write(m.vtid, annexB, ms, ms); err != nil {
		log.Printf("写 H264 sample 失败: %v", err)
	}
}

// OnAAC 写入一帧 AAC（ADTS 格式），pts 为相对起始的时间戳
func (m *MP4Muxer) OnAAC(adts []byte, pts time.Duration) {
	if m.atid == 0 {
		return
	}
	ms := uint64(pts.Milliseconds())
	if err := m.muxer.Write(m.atid, adts, ms, ms); err != nil {
		log.Printf("写 AAC sample 失败: %v", err)
	}
}

// Close 关闭 muxer，写 moov 尾部并关闭文件
func (m *MP4Muxer) Close() error {
	if err := m.muxer.WriteTrailer(); err != nil {
		log.Printf("写 MP4 trailer 失败: %v", err)
	}
	return m.file.Close()
}
