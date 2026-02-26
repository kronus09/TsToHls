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

	// 确保必要目录存在
	os.MkdirAll(TempDir, 0755)
	os.MkdirAll(filepath.Join("m3u", "logos"), 0755)

	// --- 路由设置 ---

	// 1. 静态资源路由 (映射 web/static 文件夹)
	staticFS := http.FileServer(http.Dir(filepath.Join("web", "static")))
	http.Handle("/static/", http.StripPrefix("/static/", staticFS))

	// 2. 本地图标路由 (映射 m3u/logos 文件夹)
	logoFS := http.FileServer(http.Dir(filepath.Join("m3u", "logos")))
	http.Handle("/logos/", http.StripPrefix("/logos/", logoFS))

	// 3. 前端首页
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, filepath.Join("web", "index.html"))
			return
		}
		http.NotFound(w, r)
	})

	// 4. API 接口
	http.HandleFunc("/api/upload", uploadHandler)
	http.HandleFunc("/api/list", listHandler)
	http.HandleFunc("/api/status", statusHandler)

	// 5. 资源接口
	http.HandleFunc("/playlist/tstohls.m3u", playlistHandler)
	http.HandleFunc("/stream/", streamHandler)

	fmt.Println("-------------------------------------------")
	fmt.Printf("🚀 TsToHls v1.2.0 服务已启动\n")
	fmt.Printf("👉 管理界面: http://127.0.0.1:%s\n", Port)
	fmt.Printf("👉 订阅地址: http://127.0.0.1:%s/playlist/tstohls.m3u\n", Port)
	fmt.Println("-------------------------------------------")

	log.Fatal(http.ListenAndServe(":"+Port, nil))
}

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
	addr := "http://" + r.Host
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

func listHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	// 禁用缓存，确保每次获取最新数据
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Surrogate-Control", "no-store")
	data, err := os.ReadFile("m3u/mapping.json")
	if err != nil {
		w.Write([]byte("[]"))
		return
	}
	w.Write(data)
}

func playlistHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/mpegurl")
	http.ServeFile(w, r, "m3u/tstohls.m3u")
}

func streamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	p := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(p) < 3 {
		http.NotFound(w, r)
		return
	}
	id, file := p[1], p[2]
	pm.KeepAlive(id)
	if strings.HasSuffix(file, ".m3u8") {
		content, err := pm.GetM3u8Content(id, TempDir)
		if err != nil {
			fmt.Printf("❌ 流启动失败 [%s]: %v\n", id, err)
			http.Error(w, "流启动失败: "+err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Write(content)
	} else {
		tsPath := filepath.Join(TempDir, id, file)
		http.ServeFile(w, r, tsPath)
	}
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	data := struct {
		ActiveCount int      `json:"active_count"`
		RunningIDs  []string `json:"running_ids"`
	}{
		ActiveCount: pm.GetActiveCount(),
		RunningIDs:  pm.GetProcesses(),
	}
	json.NewEncoder(w).Encode(data)
}
