package manager

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// ProcessInfo FFmpeg进程信息结构体
// 包含进程对象、最后访问时间和频道ID
type ProcessInfo struct {
	Cmd        *exec.Cmd // FFmpeg进程对象
	LastAccess time.Time // 最后访问时间，用于LRU清理
	ChannelID  string    // 频道唯一标识符
}

// ProcessManager FFmpeg进程管理器
// 负责管理所有FFmpeg转码进程的生命周期
type ProcessManager struct {
	sync.RWMutex                         // 读写锁，保护并发安全
	Processes    map[string]*ProcessInfo // 存储所有活跃的FFmpeg进程
	MaxProcesses int                     // 最大并发进程数，默认为5
}

// NewProcessManager 创建新的进程管理器实例
// 初始化时会启动后台Goroutine进行自动清理
func NewProcessManager() *ProcessManager {
	pm := &ProcessManager{
		Processes:    make(map[string]*ProcessInfo),
		MaxProcesses: 5,
	}

	// 启动后台Goroutine，每60秒扫描一次进行自动清理
	go pm.cleanupLoop()

	return pm
}

// StartProcess 启动FFmpeg转码进程
// 参数：
//   - id: 频道唯一标识符
//   - inputURL: 输入流URL（可以是HTTP、RTSP、RTMP等）
//   - outputDir: HLS输出目录
//
// 返回值：
//   - error: 启动失败时返回错误信息
func (pm *ProcessManager) StartProcess(id, inputURL, outputDir string) error {
	pm.Lock()
	defer pm.Unlock()

	// 检查进程是否已存在
	if info, exists := pm.Processes[id]; exists {
		// 进程已存在，更新最后访问时间并返回
		info.LastAccess = time.Now()
		fmt.Printf("[ProcessManager] 进程 %s 已存在，更新最后访问时间\n", id)
		return nil
	}

	// 如果进程数超过最大限制，执行LRU清理
	if len(pm.Processes) >= pm.MaxProcesses {
		pm.killOldestProcess()
	}

	// 创建输出目录
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("创建输出目录失败: %v", err)
	}

	// 构建FFmpeg命令参数
	// 格式: ffmpeg -i [inputURL] -c:v copy -c:a aac -f hls -hls_time 4 -hls_list_size 5 -hls_flags delete_segments [outputDir]/index.m3u8
	outputPath := filepath.Join(outputDir, "index.m3u8")
	cmd := exec.Command("ffmpeg",
		"-i", inputURL, // 输入流URL
		"-c:v", "copy", // 视频编码保持原样
		"-c:a", "aac", // 音频转为AAC编码
		"-f", "hls", // 输出格式为HLS
		"-hls_time", "4", // 每个HLS切片时长4秒
		"-hls_list_size", "5", // 播放列表最多保留5个切片
		"-hls_flags", "delete_segments", // 自动删除过期的切片文件
		outputPath, // 输出文件路径
	)

	// 设置工作目录
	cmd.Dir = ""

	// 启动FFmpeg进程
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动FFmpeg进程失败: %v", err)
	}

	// 记录进程信息到Map
	pm.Processes[id] = &ProcessInfo{
		Cmd:        cmd,
		LastAccess: time.Now(),
		ChannelID:  id,
	}

	fmt.Printf("[ProcessManager] 成功启动进程 %s，输入: %s，输出: %s\n", id, inputURL, outputPath)

	// 启动后台Goroutine等待进程结束
	go func() {
		cmd.Wait()
		pm.Lock()
		delete(pm.Processes, id)
		pm.Unlock()
		fmt.Printf("[ProcessManager] 进程 %s 已结束\n", id)
	}()

	return nil
}

// KeepAlive 更新指定频道的最后访问时间
// 参数：
//   - id: 频道唯一标识符
func (pm *ProcessManager) KeepAlive(id string) {
	pm.Lock()
	defer pm.Unlock()

	if info, exists := pm.Processes[id]; exists {
		info.LastAccess = time.Now()
		fmt.Printf("[ProcessManager] 更新进程 %s 的最后访问时间\n", id)
	}
}

// killOldestProcess 杀掉LastAccess最早的进程（LRU算法）
// 必须在持有写锁的情况下调用
func (pm *ProcessManager) killOldestProcess() {
	if len(pm.Processes) == 0 {
		return
	}

	// 找到LastAccess最早的进程
	var oldestID string
	var oldestTime time.Time = time.Now()

	for id, info := range pm.Processes {
		if oldestTime.IsZero() || info.LastAccess.Before(oldestTime) {
			oldestTime = info.LastAccess
			oldestID = id
		}
	}

	// 杀掉最老的进程
	if oldestID != "" {
		if info, exists := pm.Processes[oldestID]; exists && info.Cmd != nil && info.Cmd.Process != nil {
			info.Cmd.Process.Kill()
			fmt.Printf("[ProcessManager] 达到最大进程数 %d，杀掉最老进程 %s\n", pm.MaxProcesses, oldestID)
		}
		delete(pm.Processes, oldestID)
	}
}

// cleanupLoop 后台清理Goroutine
// 每60秒扫描一次Map，清理超过3分钟无活动的进程
func (pm *ProcessManager) cleanupLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		pm.cleanup()
	}
}

// cleanup 清理超过3分钟无活动的进程
func (pm *ProcessManager) cleanup() {
	pm.Lock()
	defer pm.Unlock()

	now := time.Now()
	timeout := 3 * time.Minute

	for id, info := range pm.Processes {
		if now.Sub(info.LastAccess) > timeout {
			// 杀掉超时进程
			if info.Cmd != nil && info.Cmd.Process != nil {
				info.Cmd.Process.Kill()
				fmt.Printf("[ProcessManager] 进程 %s 超过3分钟无活动，已杀掉\n", id)
			}
			delete(pm.Processes, id)
		}
	}
}

// GetProcessCount 获取当前活跃进程数量
func (pm *ProcessManager) GetProcessCount() int {
	pm.RLock()
	defer pm.RUnlock()
	return len(pm.Processes)
}

// GetProcesses 获取所有活跃进程的ID列表
func (pm *ProcessManager) GetProcesses() []string {
	pm.RLock()
	defer pm.RUnlock()

	ids := make([]string, 0, len(pm.Processes))
	for id := range pm.Processes {
		ids = append(ids, id)
	}
	return ids
}

// StopProcess 停止指定频道的FFmpeg进程
// 参数：
//   - id: 频道唯一标识符
func (pm *ProcessManager) StopProcess(id string) error {
	pm.Lock()
	defer pm.Unlock()

	info, exists := pm.Processes[id]
	if !exists {
		return fmt.Errorf("进程 %s 不存在", id)
	}

	if info.Cmd != nil && info.Cmd.Process != nil {
		if err := info.Cmd.Process.Kill(); err != nil {
			return fmt.Errorf("杀掉进程失败: %v", err)
		}
	}

	delete(pm.Processes, id)
	fmt.Printf("[ProcessManager] 已停止进程 %s\n", id)
	return nil
}
