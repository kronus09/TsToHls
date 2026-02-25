package manager

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type ProcessInfo struct {
	Cmd        *exec.Cmd
	LastAccess time.Time
	ChannelID  string
	OutputDir  string
}

type ProcessManager struct {
	sync.RWMutex
	Processes    map[string]*ProcessInfo
	MaxProcesses int
	MappingPath  string
}

func NewProcessManager() *ProcessManager {
	pm := &ProcessManager{
		Processes:    make(map[string]*ProcessInfo),
		MaxProcesses: 6,
		MappingPath:  "m3u/mapping.json",
	}
	go pm.cleanupLoop()
	return pm
}

// getRawUrl 保持不变
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
	// 等待 FFmpeg 生成首个索引文件
	for i := 0; i < 60; i++ {
		if c, err := os.ReadFile(m3u8Path); err == nil {
			return c, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, fmt.Errorf("等待 HLS 切片生成超时，请检查控制台 FFmpeg 报错")
}

func (pm *ProcessManager) ensureProcess(id, out string) error {
	pm.Lock()
	defer pm.Unlock()

	if _, ok := pm.Processes[id]; ok {
		return nil
	}
	if len(pm.Processes) >= pm.MaxProcesses {
		pm.killOldest()
	}

	raw, err := pm.getRawUrl(id)
	if err != nil {
		return err
	}

	// 【修改1】准备干净的工作目录：直接全删，杜绝任何 index.m3u8 残留
	os.RemoveAll(out)
	if err := os.MkdirAll(out, 0755); err != nil {
		return fmt.Errorf("无法创建目录: %v", err)
	}

	// 【修改2】FFmpeg 参数优化
	// 针对 Docker 组播环境增加了重试和独立切片标志
	cmd := exec.Command("ffmpeg",
		"-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5",
		"-i", raw,
		"-c:v", "copy",
		"-c:a", "aac", "-b:a", "128k",
		"-f", "hls",
		"-hls_time", "2",
		"-hls_list_size", "6",
		"-hls_flags", "delete_segments+discont_start+independent_segments",
		"-hls_segment_type", "mpegts",
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
			// 这里根据你的需求，进程自然结束时也可以清理，但通常留给 cleanupLoop 更稳
		}
		pm.Unlock()
	}()
	return nil
}

func (pm *ProcessManager) killOldest() {
	var oID string
	var oT time.Time = time.Now()
	for id, info := range pm.Processes {
		if oID == "" || info.LastAccess.Before(oT) {
			oT = info.LastAccess
			oID = id
		}
	}
	if oID != "" {
		p := pm.Processes[oID]
		if p.Cmd.Process != nil {
			fmt.Printf("🗑️  释放旧进程以达到并发上限: %s\n", oID)
			p.Cmd.Process.Kill()
			p.Cmd.Wait() // 确保进程彻底退出
		}
		delete(pm.Processes, oID)
		os.RemoveAll(p.OutputDir) // 立即清理物理文件
	}
}

func (pm *ProcessManager) KeepAlive(id string) {
	pm.Lock()
	defer pm.Unlock()
	if i, ok := pm.Processes[id]; ok {
		i.LastAccess = time.Now()
	}
}

func (pm *ProcessManager) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	for range ticker.C {
		pm.Lock()
		now := time.Now()
		for id, i := range pm.Processes {
			// 如果 2 分钟没人看，就关掉它
			if now.Sub(i.LastAccess) > 2*time.Minute {
				if i.Cmd.Process != nil {
					i.Cmd.Process.Kill()
					i.Cmd.Wait()
				}
				delete(pm.Processes, id)
				os.RemoveAll(i.OutputDir) // 关键：清理物理文件，释放 Docker 空间
				fmt.Printf("🧹 已自动清理闲置流及其物理文件: %s\n", id)
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
