package handler

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

	"tstohls/internal/db"
	"tstohls/internal/manager"
	"tstohls/internal/parser"
	"tstohls/internal/prober"
	"tstohls/internal/slicer"
	"tstohls/internal/update"
	"github.com/shirou/gopsutil/v3/cpu"
)

var PM *manager.ProcessManager

var (
	lastCPU     float64
	lastCPUTime time.Time
)

func getSystemStats() (string, string) {
	now := time.Now()
	if now.Sub(lastCPUTime) >= 3*time.Second {
		cpuPercent, _ := cpu.Percent(0, false)
		if len(cpuPercent) > 0 {
			lastCPU = cpuPercent[0]
		}
		lastCPUTime = now
	}
	cpuStr := fmt.Sprintf("%.1f", lastCPU)

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memUsed := fmt.Sprintf("%d", m.Sys/1024/1024)

	return cpuStr, memUsed
}

func ConfigHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		json.NewEncoder(w).Encode(PM.Config)
		return
	}

	if r.Method == http.MethodPost {
		if r.URL.Query().Get("action") == "reset" {
			_ = os.Remove("data/config.json")
			PM.LoadConfig()
			w.Write([]byte(`{"status":"ok","message":"已恢复默认配置"}`))
			return
		}

		var newCfg manager.FFmpegConfig
		if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
			http.Error(w, "无效的配置数据", 400)
			return
		}

		PM.Config = newCfg
		PM.SaveConfig()
		w.Write([]byte(`{"status":"ok","message":"配置保存成功"}`))
		return
	}
	http.Error(w, "不支持的方法", 405)
}

func UploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持 POST 请求", 405)
		return
	}

	checkSourceReliability := true
	sourceUrl := ""

	tmpPath := filepath.Join("data", "source.m3u")
	out, err := os.Create(tmpPath)
	if err != nil {
		http.Error(w, "创建临时文件失败", 500)
		return
	}
	defer out.Close()

	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		file, _, err := r.FormFile("m3uFile")
		if err != nil {
			http.Error(w, "文件上传失败", 400)
			return
		}
		defer file.Close()
		io.Copy(out, file)
		checkSourceReliabilityStr := r.FormValue("checkSourceReliability")
		if checkSourceReliabilityStr == "false" {
			checkSourceReliability = false
		}
	} else if strings.Contains(contentType, "application/json") {
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

		if sourceUrl != "" {
			fmt.Fprintf(out, "# TsToHls-Source-URL: %s\n", sourceUrl)
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

	dbChannels := make([]db.Channel, len(channels))
	for i, ch := range channels {
		dbChannels[i] = db.Channel{
			ID:          ch.ID,
			TvgID:       ch.TvgID,
			TvgName:     ch.TvgName,
			TvgLogo:     ch.TvgLogo,
			Name:        ch.Name,
			Logo:        ch.Logo,
			Group:       ch.Group,
			Url:         ch.Url,
			VideoCodec:  ch.VideoCodec,
			AudioCodec:  ch.AudioCodec,
			Width:       ch.Width,
			Height:      ch.Height,
			FrameRate:   ch.FrameRate,
			AudioSample: ch.AudioSample,
			InputFormat: ch.InputFormat,
			Enabled:     ch.Enabled,
			FailReason:  ch.FailReason,
		}
	}
	if err := db.InsertChannels(dbChannels); err != nil {
		fmt.Printf("❌ 写入数据库失败: %v\n", err)
	}
	prober.Default.Trigger()
	if sourceUrl != "" {
		db.SaveSource(sourceUrl, tmpPath)
	}

	PM.LoadMapping()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	fmt.Fprintf(w, `{"status":"ok", "count": %d, "message": "解析完成"}`, len(channels))
}

func CheckSourceHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	sourceUrl, filePath, _ := db.GetSource()
	exists := filePath != ""

	if _, err := os.Stat(filePath); err != nil {
		exists = false
	}

	result := struct {
		Exists    bool   `json:"exists"`
		SourceUrl string `json:"sourceUrl"`
	}{
		Exists:    exists,
		SourceUrl: sourceUrl,
	}

	json.NewEncoder(w).Encode(result)
}

func ReprocessHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持 POST 请求", 405)
		return
	}

	var req struct {
		CheckSourceReliability bool `json:"checkSourceReliability"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", 400)
		return
	}

	tmpPath := filepath.Join("data", "source.m3u")

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

	dbChannels := make([]db.Channel, len(channels))
	for i, ch := range channels {
		dbChannels[i] = db.Channel{
			ID:          ch.ID,
			TvgID:       ch.TvgID,
			TvgName:     ch.TvgName,
			TvgLogo:     ch.TvgLogo,
			Name:        ch.Name,
			Logo:        ch.Logo,
			Group:       ch.Group,
			Url:         ch.Url,
			VideoCodec:  ch.VideoCodec,
			AudioCodec:  ch.AudioCodec,
			Width:       ch.Width,
			Height:      ch.Height,
			FrameRate:   ch.FrameRate,
			AudioSample: ch.AudioSample,
			InputFormat: ch.InputFormat,
			Enabled:     ch.Enabled,
			FailReason:  ch.FailReason,
		}
	}
	if err := db.InsertChannels(dbChannels); err != nil {
		fmt.Printf("❌ 写入数据库失败: %v\n", err)
	}
	prober.Default.Trigger()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	fmt.Fprintf(w, `{"status":"ok", "count": %d, "message": "解析完成"}`, len(channels))
}

func ListHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")

	channels, err := db.GetAllChannels()
	if err != nil {
		w.Write([]byte("[]"))
		return
	}
	if channels == nil {
		channels = []db.Channel{}
	}
	json.NewEncoder(w).Encode(channels)
}

func PlaylistHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/mpegurl")
	http.ServeFile(w, r, "data/tstohls.m3u")
}

func StreamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	p := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(p) < 3 {
		http.NotFound(w, r)
		return
	}
	id, file := p[1], p[2]

	sl, err := slicer.Default.GetOrCreate(id)
	if err != nil {
		http.Error(w, "流启动失败: "+err.Error(), 500)
		return
	}
	sl.KeepAlive()

	if strings.HasSuffix(file, ".m3u8") {
		store := sl.GetStore()
		deadline := time.Now().Add(5 * time.Second)
		for store.SegmentCount() == 0 && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
		if store.SegmentCount() == 0 {
			http.Error(w, "流尚未就绪", 503)
			return
		}
		content := store.GetM3U8()
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Write(content)
	} else if strings.HasSuffix(file, ".ts") {
		var segIndex int
		fmt.Sscanf(file, "seg%05d.ts", &segIndex)
		store := sl.GetStore()
		deadline := time.Now().Add(5 * time.Second)
		var data []byte
		var ok bool
		for {
			data, ok = store.GetSegment(segIndex)
			if ok || time.Now().After(deadline) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "video/mp2t")
		w.Write(data)
	} else {
		http.NotFound(w, r)
	}
}

func ProxyHandler(w http.ResponseWriter, r *http.Request) {
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
		if strings.Contains(err.Error(), "connection was aborted") || strings.Contains(err.Error(), "broken pipe") {
			return
		}
		log.Printf("代理响应拷贝失败: %v", err)
	}
}

func StatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	cpuUsage, memUsage := getSystemStats()

	totalCount, _ := db.GetChannelCount()
	enabledCount, _ := db.GetEnabledCount()

	data := struct {
		ActiveCount     int      `json:"active_count"`
		RunningIDs      []string `json:"running_ids"`
		CPU             string   `json:"cpu"`
		Mem             string   `json:"mem"`
		TotalCount      int      `json:"total_count"`
		EnabledCount    int      `json:"enabled_count"`
		MigrationStatus string   `json:"migration_status"`
	}{
		ActiveCount:     slicer.Default.GetActiveCount(),
		RunningIDs:      slicer.Default.GetActiveIDs(),
		CPU:             cpuUsage,
		Mem:             memUsage,
		TotalCount:      totalCount,
		EnabledCount:    enabledCount,
		MigrationStatus: update.Status(),
	}
	json.NewEncoder(w).Encode(data)
}

func ChannelToggleHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持 POST 请求", 405)
		return
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", 400)
		return
	}

	newState, err := db.ToggleChannel(req.ID)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	PM.LoadMapping()

	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"enabled": newState,
		"id":      req.ID,
	})
}

func ChannelSetEnabledHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持 POST 请求", 405)
		return
	}

	var req struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", 400)
		return
	}

	if err := db.SetChannelEnabled(req.ID, req.Enabled); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	PM.LoadMapping()

	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"enabled": req.Enabled,
		"id":      req.ID,
	})
}
