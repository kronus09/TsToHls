package prober

import (
	"fmt"
	"strings"
	"sync/atomic"

	"tstohls/internal/db"
	"tstohls/internal/probe"
)

type BackgroundProber struct {
	trigger chan struct{}
	running atomic.Bool
}

var Default = &BackgroundProber{
	trigger: make(chan struct{}, 1),
}

func (p *BackgroundProber) Start() {
	go func() {
		for range p.trigger {
			if p.running.CompareAndSwap(false, true) {
				go p.run()
			}
		}
	}()
}

func (p *BackgroundProber) Trigger() {
	select {
	case p.trigger <- struct{}{}:
	default:
	}
}

func (p *BackgroundProber) IsRunning() bool {
	return p.running.Load()
}

func (p *BackgroundProber) run() {
	defer p.running.Store(false)

	channels, err := db.GetChannelsByFailReason("待后台探测")
	if err != nil || len(channels) == 0 {
		return
	}

	fmt.Printf("🔍 后台探测启动: %d 个待探测频道\n", len(channels))

	for _, ch := range channels {
		info := probe.ProbeStreamFull(ch.Url)

		codec := strings.ToLower(info.VideoCodec)
		if info.Valid && !strings.Contains(codec, "h264") && !strings.Contains(codec, "avc") {
			info.Valid = false
			info.FailReason = "非H264编码"
		}

		if !info.Valid && info.FailReason == "" {
			info.FailReason = "探测失败"
		}

		updated := db.Channel{
			VideoCodec:  info.VideoCodec,
			AudioCodec:  info.AudioCodec,
			Width:       info.Width,
			Height:      info.Height,
			FrameRate:   info.FrameRate,
			AudioSample: info.AudioSample,
			InputFormat: info.InputFormat,
			Enabled:     info.Valid,
			FailReason:  info.FailReason,
		}

		if err := db.UpdateChannelMeta(ch.ID, updated); err != nil {
			fmt.Printf("⚠️ 后台探测写入失败 %s: %v\n", ch.ID, err)
			continue
		}

		if info.Valid {
			fmt.Printf("✅ 后台探测通过: %s [%s %dx%d]\n", ch.ID, info.VideoCodec, info.Width, info.Height)
		} else {
			fmt.Printf("❌ 后台探测失败: %s %s\n", ch.ID, info.FailReason)
		}
	}

	fmt.Println("🔍 后台探测完成")
}
