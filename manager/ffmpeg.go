package manager

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FFmpegConfig 定义了可调节的性能和转换参数
type FFmpegConfig struct {
	MaxProcesses           int    `json:"max_processes"`
	HlsTime                int    `json:"hls_time"`
	HlsListSize            int    `json:"hls_list_size"`
	IdleTimeout            int    `json:"idle_timeout"`
	ReconnectDelay         int    `json:"reconnect_delay"`
	AudioCodec             string `json:"audio_codec"`
	AudioBitrate           string `json:"audio_bitrate"`
	HlsTempDir             string `json:"hls_temp_dir"`
	LowLatencyMode         bool   `json:"low_latency_mode"`
	EnableVideoTranscode   bool   `json:"enable_video_transcode"`
	VideoCodec             string `json:"video_codec"`
	VideoResolution        string `json:"video_resolution"`
	VideoBitrate           string `json:"video_bitrate"`
	HlsSegmentType         string `json:"hls_segment_type"`
	HlsFlags               string `json:"hls_flags"`
	CustomFFmpegArgs       string `json:"custom_ffmpeg_args"`
	CheckSourceReliability bool   `json:"check_source_reliability"`
}

type ProcessInfo struct {
	Cmd        *exec.Cmd
	LastAccess time.Time
	ChannelID  string
	OutputDir  string
}

type ProcessManager struct {
	sync.RWMutex
	Processes   map[string]*ProcessInfo
	Config      FFmpegConfig
	MappingPath string
	ConfigPath  string
	channels    map[string]*ChannelInfo
}

func NewProcessManager() *ProcessManager {
	pm := &ProcessManager{
		Processes:   make(map[string]*ProcessInfo),
		MappingPath: "m3u/mapping.json",
		ConfigPath:  "m3u/config.json",
		channels:    make(map[string]*ChannelInfo),
	}

	pm.LoadConfig()
	pm.LoadMapping()

	go pm.cleanupLoop()
	return pm
}

// LoadConfig 从 JSON 加载配置，如果失败则使用默认值并保存
func (pm *ProcessManager) LoadConfig() {
	defaultCfg := FFmpegConfig{
		MaxProcesses:           6,
		HlsTime:                1,
		HlsListSize:            3,
		IdleTimeout:            120,
		ReconnectDelay:         5,
		AudioCodec:             "aac",
		AudioBitrate:           "128k",
		HlsTempDir:             "",
		LowLatencyMode:         true,
		EnableVideoTranscode:   false,
		VideoCodec:             "libx264",
		VideoResolution:        "1280x720",
		VideoBitrate:           "3M",
		HlsSegmentType:         "mpegts",
		HlsFlags:               "delete_segments+discont_start+independent_segments",
		CustomFFmpegArgs:       "",
		CheckSourceReliability: true,
	}

	data, err := os.ReadFile(pm.ConfigPath)
	if err != nil {
		fmt.Printf("⚠️ 未找到配置文件，创建默认配置: %v\n", pm.ConfigPath)
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
	if pm.Config.VideoCodec == "" {
		pm.Config.VideoCodec = defaultCfg.VideoCodec
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
	if pm.Config.HlsFlags == "" {
		pm.Config.HlsFlags = defaultCfg.HlsFlags
		changed = true
	}
	if pm.Config.HlsSegmentType == "" {
		pm.Config.HlsSegmentType = defaultCfg.HlsSegmentType
		changed = true
	}

	if changed {
		pm.SaveConfig()
		fmt.Printf("📝 配置文件已更新，补充缺失字段\n")
	}
}

// SaveConfig 将当前内存中的配置保存到磁盘
func (pm *ProcessManager) SaveConfig() {
	data, _ := json.MarshalIndent(pm.Config, "", "  ")
	_ = os.WriteFile(pm.ConfigPath, data, 0644)
}

func (pm *ProcessManager) LoadMapping() {
	data, err := os.ReadFile(pm.MappingPath)
	if err != nil {
		fmt.Printf("⚠️ 未找到 mapping 文件: %v\n", err)
		return
	}

	var channels []ChannelInfo
	if err := json.Unmarshal(data, &channels); err != nil {
		fmt.Printf("❌ 解析 mapping.json 失败: %v\n", err)
		return
	}

	for i := range channels {
		pm.channels[channels[i].ID] = &channels[i]
	}
	fmt.Printf("✅ 已加载 %d 个频道到内存\n", len(pm.channels))
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

func (pm *ProcessManager) getChannelInfo(id string) (*ChannelInfo, error) {
	if info, ok := pm.channels[id]; ok {
		return info, nil
	}
	return nil, fmt.Errorf("ID [%s] 不存在", id)
}

func (pm *ProcessManager) GetM3u8Content(id, baseDir string) ([]byte, error) {
	if pm.Config.HlsTempDir != "" {
		baseDir = pm.Config.HlsTempDir
	}
	out := filepath.Join(baseDir, id)
	if err := pm.ensureProcess(id, out); err != nil {
		return nil, err
	}
	pm.KeepAlive(id)

	m3u8Path := filepath.Join(out, "index.m3u8")

	if c, err := os.ReadFile(m3u8Path); err == nil {
		return c, nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return pm.waitForM3u8Fallback(m3u8Path)
	}
	defer watcher.Close()

	if err := watcher.Add(out); err != nil {
		return pm.waitForM3u8Fallback(m3u8Path)
	}

	timeout := time.After(30 * time.Second)
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil, fmt.Errorf("watcher 已关闭")
			}
			if event.Name == m3u8Path && (event.Op&fsnotify.Create == fsnotify.Create || event.Op&fsnotify.Write == fsnotify.Write) {
				if c, err := os.ReadFile(m3u8Path); err == nil {
					return c, nil
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil, fmt.Errorf("watcher 错误: %v", err)
			}
			fmt.Printf("⚠️ fsnotify 错误: %v\n", err)
		case <-timeout:
			return nil, fmt.Errorf("等待 HLS 切片生成超时")
		}
	}
}

func (pm *ProcessManager) waitForM3u8Fallback(m3u8Path string) ([]byte, error) {
	for i := 0; i < 60; i++ {
		if c, err := os.ReadFile(m3u8Path); err == nil {
			return c, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, fmt.Errorf("等待 HLS 切片生成超时")
}

func (pm *ProcessManager) ensureProcess(id, out string) error {
	pm.Lock()
	defer pm.Unlock()

	if _, ok := pm.Processes[id]; ok {
		return nil
	}

	if len(pm.Processes) >= pm.Config.MaxProcesses {
		pm.killOldest()
	}

	info, err := pm.getChannelInfo(id)
	if err != nil {
		return err
	}

	os.RemoveAll(out)
	if err := os.MkdirAll(out, 0755); err != nil {
		return fmt.Errorf("无法创建目录: %v", err)
	}

	args := []string{
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", strconv.Itoa(pm.Config.ReconnectDelay),
	}

	if pm.Config.EnableVideoTranscode && pm.Config.CustomFFmpegArgs != "" {
		customArgs := strings.Fields(pm.Config.CustomFFmpegArgs)
		args = append(args, customArgs...)
	}

	args = append(args, "-i", info.Url)

	if !pm.Config.EnableVideoTranscode {
		args = append(args, "-c:v", "copy")
	} else {
		args = append(args, "-c:v", pm.Config.VideoCodec)
		if pm.Config.VideoResolution != "" && pm.Config.VideoResolution != "keep" {
			args = append(args, "-s", pm.Config.VideoResolution)
		}
		if pm.Config.VideoBitrate != "" {
			args = append(args, "-b:v", pm.Config.VideoBitrate)
		}
		args = append(args, "-preset", "ultrafast")
		args = append(args, "-tune", "zerolatency")
	}

	if pm.Config.AudioCodec == "copy" && info.AudioCodec != "" {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, "-c:a", pm.Config.AudioCodec)
		if pm.Config.AudioBitrate != "" {
			args = append(args, "-b:a", pm.Config.AudioBitrate)
		}
	}

	if pm.Config.LowLatencyMode {
		args = append(args,
			"-fflags", "nobuffer",
			"-flags", "low_delay",
		)
	}

	hlsFlags := pm.Config.HlsFlags
	if !strings.Contains(hlsFlags, "temp_file") {
		hlsFlags += "+temp_file"
	}

	args = append(args,
		"-f", "hls",
		"-hls_time", strconv.Itoa(pm.Config.HlsTime),
		"-hls_list_size", strconv.Itoa(pm.Config.HlsListSize),
		"-hls_flags", hlsFlags,
		"-hls_segment_type", pm.Config.HlsSegmentType,
		filepath.Join(out, "index.m3u8"),
	)

	fmt.Printf("🎬 启动 FFmpeg: %s\n", strings.Join(args, " "))

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	pm.Processes[id] = &ProcessInfo{
		Cmd:        cmd,
		LastAccess: time.Now(),
		ChannelID:  id,
		OutputDir:  out,
	}

	go func() {
		cmd.Wait()
		pm.Lock()
		if p, ok := pm.Processes[id]; ok && p.Cmd == cmd {
			delete(pm.Processes, id)
		}
		pm.Unlock()
	}()
	return nil
}

func (pm *ProcessManager) killOldest() {
	var oldestID string
	var oldestTime time.Time

	for id, p := range pm.Processes {
		if oldestID == "" || p.LastAccess.Before(oldestTime) {
			oldestID = id
			oldestTime = p.LastAccess
		}
	}

	if oldestID != "" {
		p := pm.Processes[oldestID]
		p.Cmd.Process.Kill()
		delete(pm.Processes, oldestID)
		os.RemoveAll(p.OutputDir)
		fmt.Printf("⚠️ 已终止最旧的进程: %s\n", oldestID)
	}
}

func (pm *ProcessManager) KeepAlive(id string) {
	pm.Lock()
	defer pm.Unlock()

	if p, ok := pm.Processes[id]; ok {
		p.LastAccess = time.Now()
	}
}

func (pm *ProcessManager) cleanupLoop() {
	for {
		time.Sleep(30 * time.Second)
		pm.Lock()

		now := time.Now()
		for id, p := range pm.Processes {
			if now.Sub(p.LastAccess) > time.Duration(pm.Config.IdleTimeout)*time.Second {
				p.Cmd.Process.Kill()
				delete(pm.Processes, id)
				os.RemoveAll(p.OutputDir)
				fmt.Printf("⏰ 已清理闲置进程: %s\n", id)
			}
		}

		pm.Unlock()
	}
}

func (pm *ProcessManager) GetActiveCount() int {
	pm.RLock()
	defer pm.RUnlock()
	return len(pm.Processes)
}

func (pm *ProcessManager) GetProcesses() []string {
	pm.RLock()
	defer pm.RUnlock()
	var res []string
	for id := range pm.Processes {
		res = append(res, id)
	}
	return res
}
