package manager

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv" // 导入 strconv 用于数字转字符串
	"sync"
	"time"
)

// FFmpegConfig 定义了可调节的性能和转换参数
type FFmpegConfig struct {
	MaxProcesses           int    `json:"max_processes"`
	HlsTime                int    `json:"hls_time"`      // 修改为 int
	HlsListSize            int    `json:"hls_list_size"` // 修改为 int
	IdleTimeout            int    `json:"idle_timeout"`  // 单位：秒
	VideoCodec             string `json:"video_codec"`
	AudioCodec             string `json:"audio_codec"`
	AudioBitrate           string `json:"audio_bitrate"`
	ReconnectDelay         int    `json:"reconnect_delay"` // 修改为 int
	HlsFlags               string `json:"hls_flags"`
	HlsSegmentType         string `json:"hls_segment_type"`
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
}

func NewProcessManager() *ProcessManager {
	pm := &ProcessManager{
		Processes:   make(map[string]*ProcessInfo),
		MappingPath: "m3u/mapping.json",
		ConfigPath:  "m3u/config.json",
	}

	// 初始化时加载配置
	pm.LoadConfig()

	go pm.cleanupLoop()
	return pm
}

// LoadConfig 从 JSON 加载配置，如果失败则使用默认值并保存
func (pm *ProcessManager) LoadConfig() {
	defaultCfg := FFmpegConfig{
		MaxProcesses:           6,
		HlsTime:                2, // 默认值改为数字
		HlsListSize:            6, // 默认值改为数字
		IdleTimeout:            120,
		VideoCodec:             "copy",
		AudioCodec:             "aac",
		AudioBitrate:           "128k",
		ReconnectDelay:         5, // 默认值改为数字
		HlsFlags:               "delete_segments+discont_start+independent_segments",
		HlsSegmentType:         "mpegts",
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
	}
}

// SaveConfig 将当前内存中的配置保存到磁盘
func (pm *ProcessManager) SaveConfig() {
	data, _ := json.MarshalIndent(pm.Config, "", "  ")
	_ = os.WriteFile(pm.ConfigPath, data, 0644)
}

func (pm *ProcessManager) getRawUrl(id string) (string, error) {
	data, err := os.ReadFile(pm.MappingPath)
	if err != nil {
		return "", err
	}

	type Channel struct {
		ID  string `json:"id"`
		Url string `json:"url"`
	}

	var channels []Channel
	if err := json.Unmarshal(data, &channels); err != nil {
		return "", fmt.Errorf("解析 mapping.json 失败: %v", err)
	}

	for _, ch := range channels {
		if ch.ID == id {
			return ch.Url, nil
		}
	}
	return "", fmt.Errorf("ID [%s] 不存在", id)
}

func (pm *ProcessManager) GetM3u8Content(id, baseDir string) ([]byte, error) {
	out := filepath.Join(baseDir, id)
	if err := pm.ensureProcess(id, out); err != nil {
		return nil, err
	}
	pm.KeepAlive(id)

	m3u8Path := filepath.Join(out, "index.m3u8")
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

	raw, err := pm.getRawUrl(id)
	if err != nil {
		return err
	}

	os.RemoveAll(out)
	if err := os.MkdirAll(out, 0755); err != nil {
		return fmt.Errorf("无法创建目录: %v", err)
	}

	// 使用从 config.json 加载的动态参数构建命令
	// 注意：对于 int 类型，我们需要用 strconv.Itoa 转回字符串传给 ffmpeg 命令
	cmd := exec.Command("ffmpeg",
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", strconv.Itoa(pm.Config.ReconnectDelay),
		"-i", raw,
		"-c:v", pm.Config.VideoCodec,
		"-c:a", pm.Config.AudioCodec,
		"-b:a", pm.Config.AudioBitrate,
		"-f", "hls",
		"-hls_time", strconv.Itoa(pm.Config.HlsTime),
		"-hls_list_size", strconv.Itoa(pm.Config.HlsListSize),
		"-hls_flags", pm.Config.HlsFlags,
		"-hls_segment_type", pm.Config.HlsSegmentType,
		filepath.Join(out, "index.m3u8"))

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
