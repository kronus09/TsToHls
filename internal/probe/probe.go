package probe

import (
	"fmt"
	"strings"
	"time"

	"github.com/asticode/go-astiav"
)

type StreamInfo struct {
	Valid       bool
	VideoCodec  string
	AudioCodec  string
	Width       int
	Height      int
	FrameRate   string
	AudioSample int
	InputFormat string
	FailReason  string
}

type layerConfig struct {
	probeSize   int64
	analyzeDur  int64
	timeout     time.Duration
}

var layers = []layerConfig{
	{32 * 1024, 100 * 1024, 5 * time.Second},
	{256 * 1024, 500 * 1024, 8 * time.Second},
	{1024 * 1024, 1024 * 1024, 12 * time.Second},
	{5 * 1024 * 1024, 5 * 1024 * 1024, 20 * time.Second},
}

func ProbeStream(url string) StreamInfo {
	info := StreamInfo{Valid: false}

	for _, cfg := range layers {
		result, err := probeOnce(url, cfg)
		if err != nil {
			continue
		}

		info.VideoCodec = result.videoCodec
		info.Width = result.width
		info.Height = result.height
		info.FrameRate = result.frameRate
		info.InputFormat = result.inputFormat

		codec := strings.ToLower(result.videoCodec)
		if !strings.Contains(codec, "h264") && !strings.Contains(codec, "avc") {
			info.AudioCodec, info.AudioSample = probeAudio(url)
			info.FailReason = "待后台探测"
			return info
		}

		info.Valid = true

		if result.width > 0 && result.height > 0 {
			info.AudioCodec, info.AudioSample = probeAudio(url)
			return info
		}
	}

	if info.Valid {
		info.AudioCodec, info.AudioSample = probeAudio(url)
	} else {
		info.FailReason = "探测失败"
	}

	return info
}

type probeResult struct {
	videoCodec  string
	width       int
	height      int
	frameRate   string
	inputFormat string
}

func probeOnce(url string, cfg layerConfig) (probeResult, error) {
	result := probeResult{}

	fc := astiav.AllocFormatContext()
	if fc == nil {
		return result, fmt.Errorf("无法分配格式上下文")
	}
	defer fc.Free()

	ii := astiav.NewIOInterrupter()
	defer ii.Free()
	fc.SetIOInterrupter(ii)

	go func() {
		time.Sleep(cfg.timeout)
		ii.Interrupt()
	}()

	d := astiav.NewDictionary()
	defer d.Free()
	d.Set("probesize", fmt.Sprintf("%d", cfg.probeSize), 0)
	d.Set("analyzeduration", fmt.Sprintf("%d", cfg.analyzeDur), 0)

	if err := fc.OpenInput(url, nil, d); err != nil {
		return result, fmt.Errorf("打开输入失败: %w", err)
	}
	defer fc.CloseInput()

	if err := fc.FindStreamInfo(nil); err != nil {
		return result, fmt.Errorf("探测流信息失败: %w", err)
	}

	var videoStream *astiav.Stream
	for _, s := range fc.Streams() {
		if s.CodecParameters().MediaType() == astiav.MediaTypeVideo {
			videoStream = s
			break
		}
	}

	if videoStream == nil {
		return result, fmt.Errorf("未找到视频流")
	}

	cp := videoStream.CodecParameters()
	result.videoCodec = astiav.FindDecoder(cp.CodecID()).Name()
	result.width = cp.Width()
	result.height = cp.Height()

	fr := videoStream.AvgFrameRate()
	if fr.Num() > 0 && fr.Den() > 0 {
		result.frameRate = fmt.Sprintf("%d/%d", fr.Num(), fr.Den())
	}

	if fc.InputFormat() != nil {
		result.inputFormat = fc.InputFormat().Name()
	}

	return result, nil
}

func probeAudio(url string) (string, int) {
	fc := astiav.AllocFormatContext()
	if fc == nil {
		return "", 0
	}
	defer fc.Free()

	ii := astiav.NewIOInterrupter()
	defer ii.Free()
	fc.SetIOInterrupter(ii)

	go func() {
		time.Sleep(10 * time.Second)
		ii.Interrupt()
	}()

	d := astiav.NewDictionary()
	defer d.Free()
	d.Set("probesize", "256K", 0)
	d.Set("analyzeduration", "500K", 0)

	if err := fc.OpenInput(url, nil, d); err != nil {
		return "", 0
	}
	defer fc.CloseInput()

	if err := fc.FindStreamInfo(nil); err != nil {
		return "", 0
	}

	for _, s := range fc.Streams() {
		if s.CodecParameters().MediaType() == astiav.MediaTypeAudio {
			cp := s.CodecParameters()
			codecName := astiav.FindDecoder(cp.CodecID()).Name()
			sampleRate := cp.SampleRate()
			return codecName, sampleRate
		}
	}

	return "", 0
}

func ProbeStreamFull(url string) StreamInfo {
	info := StreamInfo{}

	for _, cfg := range layers {
		result, err := probeOnce(url, cfg)
		if err != nil {
			continue
		}

		info.VideoCodec = result.videoCodec
		info.Width = result.width
		info.Height = result.height
		info.FrameRate = result.frameRate
		info.InputFormat = result.inputFormat
		info.Valid = true

		if result.width > 0 && result.height > 0 {
			info.AudioCodec, info.AudioSample = probeAudio(url)
			return info
		}
	}

	if info.Valid {
		info.AudioCodec, info.AudioSample = probeAudio(url)
	} else {
		info.FailReason = "探测失败"
	}

	return info
}
