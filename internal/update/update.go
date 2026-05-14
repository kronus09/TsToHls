package update

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"tstohls/internal/db"
	"tstohls/internal/parser"
)

const statusFile = ".migration_status"

var dataDir = "data"

func SetDataDir(dir string) {
	dataDir = dir
}

func Status() string {
	data, err := os.ReadFile(filepath.Join(dataDir, statusFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func SetStatus(status string) {
	os.WriteFile(filepath.Join(dataDir, statusFile), []byte(status), 0644)
}

func clearStatus() {
	os.Remove(filepath.Join(dataDir, statusFile))
}

func NeedMigrate() bool {
	dbPath := filepath.Join(dataDir, "tstohls.db")
	if _, e := os.Stat(dbPath); e == nil {
		clearStatus()
		return false
	}
	return true
}

func RunMigrate() {
	fmt.Println("🔄 检测到 v1.3.x 数据，开始迁移升级...")

	oldM3uDir := filepath.Join(dataDir, "m3u")

	if _, e := os.Stat(oldM3uDir); os.IsNotExist(e) {
		fmt.Println("  无旧 m3u/ 子目录，跳过文件迁移")
	} else {
		moveFile(filepath.Join(oldM3uDir, "source.m3u"), filepath.Join(dataDir, "source.m3u"))
		moveFile(filepath.Join(oldM3uDir, "tstohls.m3u"), filepath.Join(dataDir, "tstohls.m3u"))

		oldLogos := filepath.Join(oldM3uDir, "logos")
		newLogos := filepath.Join(dataDir, "logos")
		if _, e := os.Stat(oldLogos); e == nil {
			mergeDir(oldLogos, newLogos)
		}

		os.RemoveAll(oldM3uDir)
		fmt.Println("  ✅ 旧 m3u/ 子目录已迁移并清理")
	}

	oldConfig := filepath.Join(dataDir, "config.json")
	os.Remove(oldConfig)
	fmt.Println("  ✅ 旧配置文件已删除，将创建新格式配置")

	oldHlsTemp := filepath.Join(dataDir, "hls_temp")
	if _, e := os.Stat(oldHlsTemp); e == nil {
		os.RemoveAll(oldHlsTemp)
		fmt.Println("  ✅ 旧 hls_temp/ 目录已清理")
	}

	sourceM3u := filepath.Join(dataDir, "source.m3u")
	if _, e := os.Stat(sourceM3u); e == nil {
		fmt.Println("  📦 自动导入历史源文件...")

		channels, parseErr := parser.ParseAndGenerate(sourceM3u, "", true)
		if parseErr != nil {
			fmt.Printf("  ⚠️ 自动导入失败: %v\n", parseErr)
		} else {
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
				fmt.Printf("  ⚠️ 写入数据库失败: %v\n", err)
			} else {
				fmt.Printf("  ✅ 自动导入完成: %d 个频道\n", len(channels))
			}
		}
	}

	clearStatus()
	fmt.Println("🔄 迁移升级完成")
}

func moveFile(src, dst string) {
	if _, e := os.Stat(src); os.IsNotExist(e) {
		return
	}
	if _, e := os.Stat(dst); e == nil {
		return
	}
	if err := os.Rename(src, dst); err != nil {
		copyFile(src, dst)
		os.Remove(src)
	}
	fmt.Printf("  移动: %s → %s\n", filepath.Base(src), filepath.Base(dst))
}

func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()

	_, err = io.Copy(d, s)
	return err
}

func mergeDir(srcDir, dstDir string) error {
	os.MkdirAll(dstDir, 0755)

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		src := filepath.Join(srcDir, entry.Name())
		dst := filepath.Join(dstDir, entry.Name())
		if _, e := os.Stat(dst); e == nil {
			continue
		}
		copyFile(src, dst)
	}

	fmt.Printf("  合并图标: %s → %s\n", srcDir, dstDir)
	return nil
}
