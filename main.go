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
	http.HandleFunc("/api/config", configHandler)

	// 5. 资源接口
	http.HandleFunc("/playlist/tstohls.m3u", playlistHandler)
	http.HandleFunc("/stream/", streamHandler)

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
