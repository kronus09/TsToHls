package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"tstohls/manager"
	"tstohls/parser"
)

var pm *manager.ProcessManager

const (
	Port    = "15140"
	TempDir = "hls_temp"
)

func main() {
	// 初始化进程管理器
	pm = manager.NewProcessManager()

	// 确保临时目录存在
	os.MkdirAll(TempDir, 0755)
	os.MkdirAll("m3u", 0755)

	// --- 路由设置 ---

	// 1. 静态资源与前端页面
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, filepath.Join("web", "index.html"))
			return
		}
		staticFile := filepath.Join("web", r.URL.Path)
		if _, err := os.Stat(staticFile); err == nil {
			http.ServeFile(w, r, staticFile)
			return
		}
		http.NotFound(w, r)
	})

	// 2. API 接口
	http.HandleFunc("/api/upload", uploadHandler) // 上传并解析 M3U
	http.HandleFunc("/api/list", listHandler)     // 获取频道列表 (从 mapping.json)
	http.HandleFunc("/api/status", statusHandler) // 获取 FFmpeg 进程状态

	// 3. 资源接口
	http.HandleFunc("/playlist/tstohls.m3u", playlistHandler) // 获取转换后的 M3U 订阅
	http.HandleFunc("/stream/", streamHandler)                // HLS 流媒体转发

	fmt.Println("-------------------------------------------")
	fmt.Printf("🚀 TsToHls 服务已启动\n")
	fmt.Printf("👉 管理界面: http://127.0.0.1:%s\n", Port)
	fmt.Printf("👉 订阅地址: http://127.0.0.1:%s/playlist/tstohls.m3u\n", Port)
	fmt.Println("-------------------------------------------")

	log.Fatal(http.ListenAndServe(":"+Port, nil))
}

// uploadHandler 处理 M3U 上传并触发 parser 解析
func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持 POST 请求", 405)
		return
	}

	file, header, err := r.FormFile("m3uFile")
	if err != nil {
		http.Error(w, "文件上传失败", 400)
		return
	}
	defer file.Close()

	fmt.Printf("📥 接收到文件: %s，开始解析并探测...\n", header.Filename)

	tmpPath := filepath.Join("m3u", "source.m3u")
	out, err := os.Create(tmpPath)
	if err != nil {
		http.Error(w, "创建临时文件失败", 500)
		return
	}
	defer out.Close()
	io.Copy(out, file)

	// 获取当前服务的基础地址，用于 M3U 文件内的 URL 生成
	addr := "http://" + r.Host

	// 调用 parser 进行解析和 ffprobe 探测 (此过程较慢)
	channels, err := parser.ParseAndGenerate(tmpPath, addr)
	if err != nil {
		fmt.Printf("❌ 解析失败: %v\n", err)
		http.Error(w, "解析失败", 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	fmt.Fprintf(w, `{"status":"ok", "count": %d, "message": "解析完成"}`, len(channels))
}

// listHandler 读取现有的 mapping.json 并返回给前端
func listHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	data, err := os.ReadFile("m3u/mapping.json")
	if err != nil {
		// 如果文件不存在，返回空数组，匹配前端期待的结构
		w.Write([]byte("[]"))
		return
	}
	w.Write(data)
}

// playlistHandler 允许用户下载转换后的 M3U
func playlistHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/mpegurl")
	http.ServeFile(w, r, "m3u/tstohls.m3u")
}

// streamHandler 处理 /stream/{id}/index.m3u8 和 .ts 切片请求
func streamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// 解析路径: /stream/ch001/index.m3u8 -> ["stream", "ch001", "index.m3u8"]
	p := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(p) < 3 {
		http.NotFound(w, r)
		return
	}
	id, file := p[1], p[2]

	// 激活或延长进程寿命
	pm.KeepAlive(id)

	if strings.HasSuffix(file, ".m3u8") {
		// 获取 HLS 主文件内容
		content, err := pm.GetM3u8Content(id, TempDir)
		if err != nil {
			fmt.Printf("❌ 流启动失败 [%s]: %v\n", id, err)
			http.Error(w, "流启动失败: "+err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Write(content)
	} else {
		// 静态服务 .ts 切片文件
		tsPath := filepath.Join(TempDir, id, file)
		http.ServeFile(w, r, tsPath)
	}
}

// statusHandler 返回系统当前的运行状态
func statusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// 使用结构体包裹数据
	data := struct {
		ActiveCount int      `json:"active_count"`
		RunningIDs  []string `json:"running_ids"`
	}{
		ActiveCount: pm.GetActiveCount(),
		RunningIDs:  pm.GetProcesses(),
	}

	// 正确序列化为 JSON
	json.NewEncoder(w).Encode(data)
}
