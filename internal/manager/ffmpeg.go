package manager

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"tstohls/internal/db"
)

type FFmpegConfig struct {
	MaxProcesses           int    `json:"max_processes"`
	HlsTime                int    `json:"hls_time"`
	HlsListSize            int    `json:"hls_list_size"`
	IdleTimeout            int    `json:"idle_timeout"`
	ReconnectDelay         int    `json:"reconnect_delay"`
	AudioCodec             string `json:"audio_codec"`
	AudioBitrate           string `json:"audio_bitrate"`
	CheckSourceReliability bool   `json:"check_source_reliability"`
}

type ProcessManager struct {
	sync.RWMutex
	Config     FFmpegConfig
	ConfigPath string
	channels   map[string]*ChannelInfo
}

type ChannelInfo struct {
	ID          string `json:"id"`
	Url         string `json:"url"`
	VideoCodec  string `json:"video_codec,omitempty"`
	AudioCodec  string `json:"audio_codec,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	FrameRate   string `json:"frame_rate,omitempty"`
	AudioSample int    `json:"audio_sample,omitempty"`
	InputFormat string `json:"input_format,omitempty"`
}

func NewProcessManager() *ProcessManager {
	pm := &ProcessManager{
		ConfigPath: "data/config.json",
		channels:   make(map[string]*ChannelInfo),
	}

	pm.LoadConfig()
	pm.LoadMapping()

	return pm
}

func (pm *ProcessManager) LoadConfig() {
	defaultCfg := FFmpegConfig{
		MaxProcesses:           6,
		HlsTime:                1,
		HlsListSize:            4,
		IdleTimeout:            120,
		ReconnectDelay:         5,
		AudioCodec:             "aac",
		AudioBitrate:           "128k",
		CheckSourceReliability: true,
	}

	data, err := os.ReadFile(pm.ConfigPath)
	if err != nil {
		pm.Config = defaultCfg
		pm.SaveConfig()
		return
	}

	if err := json.Unmarshal(data, &pm.Config); err != nil {
		fmt.Printf("❌ 解析配置文件失败，使用默认配置: %v\n", err)
		pm.Config = defaultCfg
		pm.SaveConfig()
		return
	}

	changed := false
	if pm.Config.MaxProcesses == 0 {
		pm.Config.MaxProcesses = defaultCfg.MaxProcesses
		changed = true
	}
	if pm.Config.HlsTime == 0 {
		pm.Config.HlsTime = defaultCfg.HlsTime
		changed = true
	}
	if pm.Config.HlsListSize == 0 {
		pm.Config.HlsListSize = defaultCfg.HlsListSize
		changed = true
	}
	if pm.Config.IdleTimeout == 0 {
		pm.Config.IdleTimeout = defaultCfg.IdleTimeout
		changed = true
	}
	if pm.Config.ReconnectDelay == 0 {
		pm.Config.ReconnectDelay = defaultCfg.ReconnectDelay
		changed = true
	}
	if pm.Config.AudioCodec == "" {
		pm.Config.AudioCodec = defaultCfg.AudioCodec
		changed = true
	}
	if pm.Config.AudioBitrate == "" {
		pm.Config.AudioBitrate = defaultCfg.AudioBitrate
		changed = true
	}

	if changed {
		pm.SaveConfig()
		fmt.Printf("📝 配置文件已更新，补充缺失字段\n")
	}
}

func (pm *ProcessManager) SaveConfig() {
	data, _ := json.MarshalIndent(pm.Config, "", "  ")
	_ = os.WriteFile(pm.ConfigPath, data, 0644)
}

func (pm *ProcessManager) LoadMapping() {
	channels, err := db.GetEnabledChannels()
	if err != nil {
		fmt.Printf("⚠️ 从数据库加载频道失败: %v\n", err)
		return
	}

	for i := range channels {
		pm.channels[channels[i].ID] = &ChannelInfo{
			ID:          channels[i].ID,
			Url:         channels[i].Url,
			VideoCodec:  channels[i].VideoCodec,
			AudioCodec:  channels[i].AudioCodec,
			Width:       channels[i].Width,
			Height:      channels[i].Height,
			FrameRate:   channels[i].FrameRate,
			AudioSample: channels[i].AudioSample,
			InputFormat: channels[i].InputFormat,
		}
	}
	fmt.Printf("✅ 已加载 %d 个频道到内存\n", len(pm.channels))
}
