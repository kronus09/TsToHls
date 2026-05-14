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

	channels, err := db.GetChannelsForProbing()
	if err != nil || len(channels) == 0 {
		return
	}

	fmt.Printf("🔍 后台探测启动: %d 个待探测频道\n", len(channels))

	for _, ch := range channels {
		info := probe.ProbeStreamFull(ch.Url)

		if !info.Valid && info.FailReason == "" {
			info.FailReason = "探测失败"
		}

		codec := strings.ToLower(info.VideoCodec)
		isH264 := strings.Contains(codec, "h264") || strings.Contains(codec, "avc")

		enabled := info.Valid && isH264
		failReason := info.FailReason
		if info.Valid && !isH264 {
			failReason = info.VideoCodec
		}

		updated := db.Channel{
			VideoCodec:  info.VideoCodec,
			AudioCodec:  info.AudioCodec,
			Width:       info.Width,
			Height:      info.Height,
			FrameRate:   info.FrameRate,
			AudioSample: info.AudioSample,
			InputFormat: info.InputFormat,
			Enabled:     enabled,
			FailReason:  failReason,
		}

		if err := db.UpdateChannelMeta(ch.ID, updated); err != nil {
			fmt.Printf("⚠️ 后台探测写入失败 %s: %v\n", ch.Name, err)
			continue
		}

		if enabled {
			fmt.Printf("✅ 后台探测通过: %s [%s %dx%d]\n", ch.Name, info.VideoCodec, info.Width, info.Height)
		} else {
			fmt.Printf("ℹ️ 后台探测完成: %s [%s] %s\n", ch.Name, info.VideoCodec, failReason)
		}
	}

	fmt.Println("🔍 后台探测完成")
}
