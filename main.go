package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"tstohls/internal/db"
	"tstohls/internal/handler"
	"tstohls/internal/manager"
	"tstohls/internal/prober"
	"tstohls/internal/slicer"
	"tstohls/internal/update"

	"github.com/asticode/go-astiav"
)

const (
	Port = "15140"
)

func main() {
	astiav.SetLogLevel(astiav.LogLevelFatal)

	fc := astiav.AllocFormatContext()
	if fc != nil {
		fc.Free()
	}

	os.MkdirAll("data", 0755)
	update.SetDataDir("data")

	needMigrate := update.NeedMigrate()
	if needMigrate {
		update.SetStatus("migrating")
	}

	if err := db.Init(); err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	defer db.Close()

	pm := manager.NewProcessManager()
	handler.PM = pm

	slicer.Default.Init(slicer.SlicerConfig{
		HlsTime:        float64(pm.Config.HlsTime),
		HlsListSize:    pm.Config.HlsListSize,
		IdleTimeout:    pm.Config.IdleTimeout,
		ReconnectDelay: pm.Config.ReconnectDelay,
		AudioCodec:     pm.Config.AudioCodec,
		AudioBitrate:   pm.Config.AudioBitrate,
	}, pm.Config.MaxProcesses)

	prober.Default.Start()

	os.MkdirAll(filepath.Join("data", "logos"), 0755)

	staticFS := http.FileServer(http.Dir(filepath.Join("web", "static")))
	http.Handle("/static/", http.StripPrefix("/static/", staticFS))

	pagesFS := http.FileServer(http.Dir(filepath.Join("web", "pages")))
	http.Handle("/pages/", http.StripPrefix("/pages/", pagesFS))

	logoFS := http.FileServer(http.Dir(filepath.Join("data", "logos")))
	http.Handle("/logos/", http.StripPrefix("/logos/", logoFS))

	dataFS := http.FileServer(http.Dir("data"))
	http.Handle("/data/", http.StripPrefix("/data/", dataFS))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, filepath.Join("web", "index.html"))
			return
		}
		http.NotFound(w, r)
	})

	http.HandleFunc("/api/upload", handler.UploadHandler)
	http.HandleFunc("/api/list", handler.ListHandler)
	http.HandleFunc("/api/status", handler.StatusHandler)
	http.HandleFunc("/api/config", handler.ConfigHandler)
	http.HandleFunc("/api/check-source", handler.CheckSourceHandler)
	http.HandleFunc("/api/reprocess", handler.ReprocessHandler)
	http.HandleFunc("/api/channel/toggle", handler.ChannelToggleHandler)
	http.HandleFunc("/api/channel/set-enabled", handler.ChannelSetEnabledHandler)

	http.HandleFunc("/playlist/tstohls.m3u", handler.PlaylistHandler)
	http.HandleFunc("/stream/", handler.StreamHandler)
	http.HandleFunc("/proxy/", handler.ProxyHandler)

	fmt.Println("-------------------------------------------")
	fmt.Printf("🚀 TsToHls v1.4.0 服务已启动\n")
	fmt.Printf("👉 管理界面: http://127.0.0.1:%s\n", Port)
	fmt.Printf("👉 订阅地址: http://127.0.0.1:%s/playlist/tstohls.m3u\n", Port)
	fmt.Println("-------------------------------------------")

	if needMigrate {
		go func() {
			update.RunMigrate()
			pm.LoadMapping()
			prober.Default.Trigger()
		}()
	} else {
		prober.Default.Trigger()
	}

	log.Fatal(http.ListenAndServe(":"+Port, nil))
}
