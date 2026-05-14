package parser

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"tstohls/internal/probe"
)

type Channel struct {
	ID          string `json:"id"`
	TvgID       string `json:"tvg_id,omitempty"`
	TvgName     string `json:"tvg_name,omitempty"`
	TvgLogo     string `json:"tvg_logo,omitempty"`
	Name        string `json:"name"`
	Logo        string `json:"logo"`
	Group       string `json:"group"`
	Url         string `json:"url"`
	VideoCodec  string `json:"video_codec,omitempty"`
	AudioCodec  string `json:"audio_codec,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	FrameRate   string `json:"frame_rate,omitempty"`
	AudioSample int    `json:"audio_sample,omitempty"`
	InputFormat string `json:"input_format,omitempty"`
	Enabled     bool   `json:"enabled"`
	FailReason  string `json:"fail_reason,omitempty"`
}

func downloadLogo(id, remoteURL string) string {
	if remoteURL == "" {
		return "/static/logos/favicon.png"
	}

	logoDir := filepath.Join("data", "logos")
	os.MkdirAll(logoDir, 0755)

	ext := filepath.Ext(remoteURL)
	if ext == "" || len(ext) > 5 {
		ext = ".png"
	}
	fileName := id + ext
	localPath := filepath.Join(logoDir, fileName)
	webPath := "/logos/" + fileName

	if _, err := os.Stat(localPath); err == nil {
		return webPath
	}

	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(remoteURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		return "/static/logos/favicon.png"
	}
	defer resp.Body.Close()

	out, err := os.Create(localPath)
	if err != nil {
		return "/static/logos/favicon.png"
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return "/static/logos/favicon.png"
	}

	return webPath
}

func ParseAndGenerate(inputPath, serverAddr string, checkReliability bool) ([]Channel, error) {
	outputDir := "data"
	os.MkdirAll(outputDir, 0755)

	file, err := os.Open(inputPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var rawChannels []Channel
	scanner := bufio.NewScanner(file)

	reTvgID := regexp.MustCompile(`tvg-id="([^"]*)"`)
	reTvgName := regexp.MustCompile(`tvg-name="([^"]*)"`)
	reTvgLogo := regexp.MustCompile(`tvg-logo="([^"]*)"`)
	reGroup := regexp.MustCompile(`group-title="([^"]*)"`)

	var current Channel
	idx := 1
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(strings.ToUpper(line), "EXTM3U") || strings.HasPrefix(line, "#EXTM3U") {
			continue
		}

		if strings.HasPrefix(line, "#EXTINF:") {
			if m := reTvgID.FindStringSubmatch(line); len(m) > 1 {
				current.TvgID = m[1]
			}
			if m := reTvgName.FindStringSubmatch(line); len(m) > 1 {
				current.TvgName = m[1]
			}
			if m := reTvgLogo.FindStringSubmatch(line); len(m) > 1 {
				current.TvgLogo = m[1]
			}
			if m := reGroup.FindStringSubmatch(line); len(m) > 1 {
				current.Group = m[1]
			}
			if lastComma := strings.LastIndex(line, ","); lastComma != -1 {
				current.Name = line[lastComma+1:]
			}
			if current.TvgName != "" {
				current.Name = current.TvgName
			}
		} else if !strings.HasPrefix(line, "#") {
			lowerLine := strings.ToLower(line)
			isValidProtocol := strings.HasPrefix(lowerLine, "http://") ||
				strings.HasPrefix(lowerLine, "https://") ||
				strings.HasPrefix(lowerLine, "rtp://") ||
				strings.HasPrefix(lowerLine, "udp://")

			isImage := strings.HasSuffix(lowerLine, ".png") ||
				strings.HasSuffix(lowerLine, ".jpg") ||
				strings.HasSuffix(lowerLine, ".jpeg")

			if isValidProtocol && !isImage {
				current.Url = line
				current.ID = fmt.Sprintf("ch%03d", idx)
				if current.Name == "" {
					current.Name = fmt.Sprintf("未命名-%d", idx)
				}
				rawChannels = append(rawChannels, current)
				current = Channel{}
				idx++
			}
		}
	}

	fmt.Printf("📝 预扫描完成，准备验证 %d 个视频流地址...\n", len(rawChannels))

	var wg sync.WaitGroup
	var mu sync.Mutex
	limit := make(chan struct{}, 5)
	passed := 0
	failed := 0
	deferred := 0

	for i := range rawChannels {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c := &rawChannels[idx]
			limit <- struct{}{}

			if checkReliability {
				info := probe.ProbeStream(c.Url)
				if info.Valid {
					c.VideoCodec = info.VideoCodec
					c.AudioCodec = info.AudioCodec
					c.Width = info.Width
					c.Height = info.Height
					c.FrameRate = info.FrameRate
					c.AudioSample = info.AudioSample
					c.InputFormat = info.InputFormat
					c.Enabled = true
					resolution := fmt.Sprintf("%dx%d", c.Width, c.Height)
					if c.Width == 0 || c.Height == 0 {
						resolution = "未知分辨率"
					}
					fmt.Printf("✅ 验证通过: %s [%s %s]\n", c.Name, resolution, c.VideoCodec)
					mu.Lock()
					passed++
					mu.Unlock()
				} else if info.FailReason == "待后台探测" {
					c.VideoCodec = info.VideoCodec
					c.AudioCodec = info.AudioCodec
					c.Width = info.Width
					c.Height = info.Height
					c.FrameRate = info.FrameRate
					c.AudioSample = info.AudioSample
					c.InputFormat = info.InputFormat
					c.Enabled = false
					c.FailReason = info.VideoCodec
					fmt.Printf("⏳ 待后台探测: %s [%s]\n", c.Name, c.VideoCodec)
					mu.Lock()
					deferred++
					mu.Unlock()
				} else {
					c.VideoCodec = info.VideoCodec
					c.AudioCodec = info.AudioCodec
					c.Width = info.Width
					c.Height = info.Height
					c.Enabled = false
					c.FailReason = info.FailReason
					if info.VideoCodec != "" {
						resolution := fmt.Sprintf("%dx%d", info.Width, info.Height)
						if c.Width == 0 || c.Height == 0 {
							resolution = "未知分辨率"
						}
						fmt.Printf("❌ 验证失败: %s [%s %s] %s\n", c.Name, resolution, info.VideoCodec, info.FailReason)
					} else {
						fmt.Printf("❌ 验证失败: %s %s\n", c.Name, info.FailReason)
					}
					mu.Lock()
					failed++
					mu.Unlock()
				}
			} else {
				info := probe.ProbeStream(c.Url)
				if info.Valid {
					c.VideoCodec = info.VideoCodec
					c.AudioCodec = info.AudioCodec
					c.Width = info.Width
					c.Height = info.Height
					c.FrameRate = info.FrameRate
					c.AudioSample = info.AudioSample
					c.InputFormat = info.InputFormat
					c.Enabled = true
					fmt.Printf("✅ 探测通过: %s [%dx%d %s]\n", c.Name, c.Width, c.Height, c.VideoCodec)
				} else if info.FailReason == "待后台探测" {
					c.VideoCodec = info.VideoCodec
					c.AudioCodec = info.AudioCodec
					c.Width = info.Width
					c.Height = info.Height
					c.Enabled = true
					c.FailReason = "待后台探测"
					fmt.Printf("⏳ 非H264已启用: %s\n", c.Name)
				} else {
					c.VideoCodec = info.VideoCodec
					c.AudioCodec = info.AudioCodec
					c.Width = info.Width
					c.Height = info.Height
					c.Enabled = true
					c.FailReason = info.FailReason
					fmt.Printf("⚠️ 探测失败已启用: %s %s\n", c.Name, info.FailReason)
				}
				mu.Lock()
				passed++
				mu.Unlock()
			}
			<-limit
		}(i)
	}
	wg.Wait()

	if checkReliability {
		fmt.Printf("📊 验证完成: ✅ %d 通过, ❌ %d 失败, ⏳ %d 待后台探测\n", passed, failed, deferred)
	}

	sort.Slice(rawChannels, func(i, j int) bool {
		return rawChannels[i].ID < rawChannels[j].ID
	})

	m3uPath := filepath.Join(outputDir, "tstohls.m3u")
	mFile, _ := os.Create(m3uPath)
	defer mFile.Close()
	mFile.WriteString("#EXTM3U\n")

	for _, ch := range rawChannels {
		if !ch.Enabled {
			continue
		}
		proxyUrl := fmt.Sprintf("%s/stream/%s/index.m3u8", serverAddr, ch.ID)
		mFile.WriteString(fmt.Sprintf("#EXTINF:-1 tvg-id=\"%s\" tvg-name=\"%s\" tvg-logo=\"%s\" group-title=\"%s\",%s\n%s\n",
			ch.TvgID, ch.TvgName, ch.TvgLogo, ch.Group, ch.Name, proxyUrl))
	}

	fmt.Println("🖼️ 正在同步下载频道图标至本地...")
	for i := range rawChannels {
		rawChannels[i].Logo = downloadLogo(rawChannels[i].ID, rawChannels[i].TvgLogo)
	}

	enabled := 0
	for _, ch := range rawChannels {
		if ch.Enabled {
			enabled++
		}
	}
	fmt.Printf("🚀 全部处理完成！频道: %d (启用 %d, 禁用 %d)，图标已存至 data/logos/\n", len(rawChannels), enabled, len(rawChannels)-enabled)
	return rawChannels, nil
}
