package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"tstohls/manager"
	"tstohls/parser"

	"github.com/shirou/gopsutil/v3/cpu"
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

	// 1. 静态资源路由 (js, css, logo.png)
	staticFS := http.FileServer(http.Dir(filepath.Join("web", "static")))
	http.Handle("/static/", http.StripPrefix("/static/", staticFS))

	// 2. 本地图标路由 (映射 m3u/logos 文件夹)
	logoFS := http.FileServer(http.Dir(filepath.Join("m3u", "logos")))
	http.Handle("/logos/", http.StripPrefix("/logos/", logoFS))

	// 3. M3U文件路由 (映射 m3u 文件夹，用于 Direct TS 播放)
	m3uFS := http.FileServer(http.Dir(filepath.Join("m3u")))
	http.Handle("/m3u/", http.StripPrefix("/m3u/", m3uFS))

	// 4. 前端首页
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
	http.HandleFunc("/api/config", configHandler)
	http.HandleFunc("/api/check-source", checkSourceHandler)
	http.HandleFunc("/api/reprocess", reprocessHandler)

	// 5. 资源接口
	http.HandleFunc("/playlist/tstohls.m3u", playlistHandler)
	http.HandleFunc("/stream/", streamHandler)
	http.HandleFunc("/proxy/", proxyHandler)

	fmt.Println("-------------------------------------------")
	fmt.Printf("🚀 TsToHls v1.2.2 服务已启动\n")
	fmt.Printf("👉 管理界面: http://127.0.0.1:%s\n", Port)
	fmt.Printf("👉 订阅地址: http://127.0.0.1:%s/playlist/tstohls.m3u\n", Port)
	fmt.Println("-------------------------------------------")

	log.Fatal(http.ListenAndServe(":"+Port, nil))
}

func getSystemStats() (string, string) {
	// CPU 使用率 (采样时间 200ms)
	cpuPercent, _ := cpu.Percent(200*time.Millisecond, false)
	cpuStr := "0.0"
	if len(cpuPercent) > 0 {
		cpuStr = fmt.Sprintf("%.1f", cpuPercent[0])
	}

	// 内存使用 (Runtime 版)
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memUsed := fmt.Sprintf("%d", m.Sys/1024/1024)

	return cpuStr, memUsed
}

func configHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		json.NewEncoder(w).Encode(pm.Config)
		return
	}

	if r.Method == http.MethodPost {
		if r.URL.Query().Get("action") == "reset" {
			_ = os.Remove("m3u/config.json")
			pm.LoadConfig()
			w.Write([]byte(`{"status":"ok","message":"已恢复默认配置"}`))
			return
		}

		var newCfg manager.FFmpegConfig
		if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
			http.Error(w, "无效的配置数据", 400)
			return
		}

		pm.Config = newCfg
		pm.SaveConfig()
		w.Write([]byte(`{"status":"ok","message":"配置保存成功"}`))
		return
	}
	http.Error(w, "不支持的方法", 405)
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持 POST 请求", 405)
		return
	}

	checkSourceReliability := true
	sourceUrl := ""

	tmpPath := filepath.Join("m3u", "source.m3u")
	out, err := os.Create(tmpPath)
	if err != nil {
		http.Error(w, "创建临时文件失败", 500)
		return
	}
	defer out.Close()

	// 检查请求类型
	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		// 处理文件上传
		file, _, err := r.FormFile("m3uFile")
		if err != nil {
			http.Error(w, "文件上传失败", 400)
			return
		}
		defer file.Close()
		io.Copy(out, file)
		// 处理checkSourceReliability参数
		checkSourceReliabilityStr := r.FormValue("checkSourceReliability")
		if checkSourceReliabilityStr == "false" {
			checkSourceReliability = false
		}
	} else if strings.Contains(contentType, "application/json") {
		// 处理URL请求
		var req struct {
			URL                    string `json:"url"`
			CheckSourceReliability bool   `json:"checkSourceReliability"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "无效的请求数据", 400)
			return
		}
		if req.URL == "" {
			http.Error(w, "URL不能为空", 400)
			return
		}
		sourceUrl = req.URL
		checkSourceReliability = req.CheckSourceReliability
		// 从URL下载文件
		resp, err := http.Get(req.URL)
		if err != nil {
			http.Error(w, "下载文件失败", 500)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			http.Error(w, "下载文件失败", 500)
			return
		}

		// 先写入订阅地址注释
		if sourceUrl != "" {
			fmt.Fprintf(out, "# TsToHls-Source-URL: %s\n", sourceUrl)
		}

		// 写入文件内容
		io.Copy(out, resp.Body)
	} else {
		http.Error(w, "不支持的请求类型", 400)
		return
	}

	addr := "http://" + r.Host
	channels, err := parser.ParseAndGenerate(tmpPath, addr, checkSourceReliability)
	if err != nil {
		http.Error(w, "解析失败", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	fmt.Fprintf(w, `{"status":"ok", "count": %d, "message": "解析完成"}`, len(channels))
}

// 检查source.m3u文件是否存在，并提取订阅地址
func checkSourceHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	sourcePath := filepath.Join("m3u", "source.m3u")

	// 检查文件是否存在
	exists := false
	sourceUrl := ""

	if _, err := os.Stat(sourcePath); err == nil {
		exists = true

		// 读取文件内容，提取订阅地址
		content, err := os.ReadFile(sourcePath)
		if err == nil {
			lines := strings.Split(string(content), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "# TsToHls-Source-URL:") {
					sourceUrl = strings.TrimSpace(strings.TrimPrefix(line, "# TsToHls-Source-URL:"))
					break
				}
			}
		}
	}

	// 返回结果
	result := struct {
		Exists    bool   `json:"exists"`
		SourceUrl string `json:"sourceUrl"`
	}{
		Exists:    exists,
		SourceUrl: sourceUrl,
	}

	json.NewEncoder(w).Encode(result)
}

// 直接使用source.m3u文件进行转换
func reprocessHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持 POST 请求", 405)
		return
	}

	// 解析请求参数
	var req struct {
		CheckSourceReliability bool `json:"checkSourceReliability"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", 400)
		return
	}

	tmpPath := filepath.Join("m3u", "source.m3u")

	// 检查文件是否存在
	if _, err := os.Stat(tmpPath); err != nil {
		http.Error(w, "source.m3u文件不存在", 400)
		return
	}

	addr := "http://" + r.Host
	channels, err := parser.ParseAndGenerate(tmpPath, addr, req.CheckSourceReliability)
	if err != nil {
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
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
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

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	p := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(p) < 2 {
		http.Error(w, "无效的代理请求", 400)
		return
	}

	targetURL := strings.Join(p[1:], "/")
	targetURL = strings.ReplaceAll(targetURL, "%2F", "/")
	targetURL = strings.ReplaceAll(targetURL, "%3A", ":")

	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "http://" + targetURL
	}

	client := &http.Client{}
	req, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, "请求创建失败: "+err.Error(), 500)
		return
	}

	for key, values := range r.Header {
		if key != "Host" && key != "Origin" {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "代理请求失败: "+err.Error(), 500)
		return
	}
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		// 忽略客户端关闭连接的错误
		if strings.Contains(err.Error(), "connection was aborted") || strings.Contains(err.Error(), "broken pipe") {
			// 客户端可能已经关闭了连接（例如用户切换了频道）
			return
		}
		log.Printf("代理响应拷贝失败: %v", err)
	}
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	cpuUsage, memUsage := getSystemStats()

	data := struct {
		ActiveCount int      `json:"active_count"`
		RunningIDs  []string `json:"running_ids"`
		CPU         string   `json:"cpu"`
		Mem         string   `json:"mem"`
	}{
		ActiveCount: pm.GetActiveCount(),
		RunningIDs:  pm.GetProcesses(),
		CPU:         cpuUsage,
		Mem:         memUsage,
	}
	json.NewEncoder(w).Encode(data)
}
