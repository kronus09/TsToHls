package parser

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Channel struct {
	ID          string `json:"id"`
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
}

type StreamInfo struct {
	Valid       bool
	VideoCodec  string
	AudioCodec  string
	Width       int
	Height      int
	FrameRate   string
	AudioSample int
	InputFormat string
	FailReason  string
}

// downloadLogo 下载图标到本地并返回 web 访问路径
func downloadLogo(id, remoteURL string) string {
	if remoteURL == "" {
		return "/static/logos/favicon.png"
	}

	// 准备目录
	logoDir := filepath.Join("m3u", "logos")
	os.MkdirAll(logoDir, 0755)

	// 提取后缀名
	ext := filepath.Ext(remoteURL)
	if ext == "" || len(ext) > 5 {
		ext = ".png"
	}
	fileName := id + ext
	localPath := filepath.Join(logoDir, fileName)
	webPath := "/logos/" + fileName

	// 如果文件已存在则跳过
	if _, err := os.Stat(localPath); err == nil {
		return webPath
	}

	// 限制 5 秒超时下载
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

type probeResult struct {
	Success    bool
	VideoCodec string
	Width      int
	Height     int
	FrameRate  string
	FormatName string
}

func probeOnce(url, probesize, analyzeduration string, timeout time.Duration) probeResult {
	result := probeResult{Success: false}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-probesize", probesize,
		"-analyzeduration", analyzeduration,
		"-show_entries", "stream=codec_name,width,height,r_frame_rate:format=format_name",
		"-of", "json",
		"-select_streams", "v:0",
		url)

	out, err := cmd.Output()
	if err != nil {
		return result
	}

	var parsed struct {
		Streams []struct {
			CodecName string `json:"codec_name"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			FrameRate string `json:"r_frame_rate"`
		} `json:"streams"`
		Format struct {
			FormatName string `json:"format_name"`
		} `json:"format"`
	}

	if err := json.Unmarshal(out, &parsed); err != nil {
		return result
	}

	if len(parsed.Streams) == 0 {
		return result
	}

	stream := parsed.Streams[0]
	result.Success = true
	result.VideoCodec = stream.CodecName
	result.Width = stream.Width
	result.Height = stream.Height
	result.FrameRate = stream.FrameRate
	result.FormatName = parsed.Format.FormatName

	return result
}

func probeAudio(url string) (string, int) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-probesize", "256K",
		"-analyzeduration", "500K",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name,sample_rate",
		"-of", "json",
		url)

	out, err := cmd.Output()
	if err != nil {
		return "", 0
	}

	var result struct {
		Streams []struct {
			CodecName  string `json:"codec_name"`
			SampleRate string `json:"sample_rate"`
		} `json:"streams"`
	}

	if json.Unmarshal(out, &result) != nil || len(result.Streams) == 0 {
		return "", 0
	}

	var sampleRate int
	if result.Streams[0].SampleRate != "" {
		fmt.Sscanf(result.Streams[0].SampleRate, "%d", &sampleRate)
	}

	return result.Streams[0].CodecName, sampleRate
}

// ProbeStream: 分层探测流信息
// 第一层: 32K 极速探测 → 第二层: 256K 快速探测 → 第三层: 1M 标准探测
func ProbeStream(url string) StreamInfo {
	info := StreamInfo{Valid: false}

	layers := []struct {
		probesize      string
		analyzedur     string
		timeout        time.Duration
		desc           string
	}{
		{"32K", "100K", 5 * time.Second, "32K"},
		{"256K", "500K", 8 * time.Second, "256K"},
		{"1M", "1M", 12 * time.Second, "1M"},
		{"5M", "5M", 20 * time.Second, "5M"},
	}

	for _, layer := range layers {
		result := probeOnce(url, layer.probesize, layer.analyzedur, layer.timeout)

		if !result.Success {
			continue
		}

		codec := strings.ToLower(result.VideoCodec)
		if !strings.Contains(codec, "h264") && !strings.Contains(codec, "avc") {
			info.VideoCodec = result.VideoCodec
			info.Width = result.Width
			info.Height = result.Height
			info.FailReason = "非H264编码"
			return info
		}

		info.VideoCodec = result.VideoCodec
		info.Width = result.Width
		info.Height = result.Height
		info.FrameRate = result.FrameRate
		info.InputFormat = result.FormatName
		info.Valid = true

		if result.Width > 0 && result.Height > 0 {
			info.AudioCodec, info.AudioSample = probeAudio(url)
			return info
		}
	}

	if info.Valid {
		info.AudioCodec, info.AudioSample = probeAudio(url)
	} else {
		info.FailReason = "探测失败"
	}

	return info
}

// ValidateStream：探测并只保留 H.264 视频流（保留兼容性）
func ValidateStream(url string) bool {
	info := ProbeStream(url)
	return info.Valid
}

func ParseAndGenerate(inputPath, serverAddr string, checkReliability bool) ([]Channel, error) {
	outputDir := "m3u"
	os.MkdirAll(outputDir, 0755)

	file, err := os.Open(inputPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var rawChannels []Channel
	scanner := bufio.NewScanner(file)

	reName := regexp.MustCompile(`tvg-name="([^"]*)"`)
	reLogo := regexp.MustCompile(`tvg-logo="([^"]*)"`)
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
			if lastComma := strings.LastIndex(line, ","); lastComma != -1 {
				current.Name = line[lastComma+1:]
			}
			if m := reName.FindStringSubmatch(line); len(m) > 1 {
				current.Name = m[1]
			}
			if m := reLogo.FindStringSubmatch(line); len(m) > 1 {
				current.Logo = m[1]
			}
			if m := reGroup.FindStringSubmatch(line); len(m) > 1 {
				current.Group = m[1]
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

	var validChannels []Channel
	var wg sync.WaitGroup
	var mu sync.Mutex
	limit := make(chan struct{}, 5)

	for _, ch := range rawChannels {
		wg.Add(1)
		go func(c Channel) {
			defer wg.Done()
			limit <- struct{}{}

			if checkReliability {
				info := ProbeStream(c.Url)
				if info.Valid {
					c.VideoCodec = info.VideoCodec
					c.AudioCodec = info.AudioCodec
					c.Width = info.Width
					c.Height = info.Height
					c.FrameRate = info.FrameRate
					c.AudioSample = info.AudioSample
					c.InputFormat = info.InputFormat
					mu.Lock()
					validChannels = append(validChannels, c)
					mu.Unlock()
					resolution := fmt.Sprintf("%dx%d", c.Width, c.Height)
					if c.Width == 0 || c.Height == 0 {
						resolution = "未知分辨率"
					}
					fmt.Printf("✅ 验证通过: %s [%s %s]\n", c.Name, resolution, c.VideoCodec)
				} else {
					if info.VideoCodec != "" {
						resolution := fmt.Sprintf("%dx%d", info.Width, info.Height)
						if info.Width == 0 || info.Height == 0 {
							resolution = "未知分辨率"
						}
						fmt.Printf("❌ 验证失败: %s [%s %s] %s\n", c.Name, resolution, info.VideoCodec, info.FailReason)
					} else {
						fmt.Printf("❌ 验证失败: %s %s\n", c.Name, info.FailReason)
					}
				}
			} else {
				mu.Lock()
				validChannels = append(validChannels, c)
				mu.Unlock()
				fmt.Printf("✅ 跳过验证: %s\n", c.Name)
			}
			<-limit
		}(ch)
	}
	wg.Wait()

	sort.Slice(validChannels, func(i, j int) bool {
		return validChannels[i].ID < validChannels[j].ID
	})

	// 1. 生成订阅 m3u (这里保持使用原始远程 Logo 地址)
	m3uPath := filepath.Join(outputDir, "tstohls.m3u")
	mFile, _ := os.Create(m3uPath)
	defer mFile.Close()
	mFile.WriteString("#EXTM3U\n")

	for _, ch := range validChannels {
		proxyUrl := fmt.Sprintf("%s/stream/%s/index.m3u8", serverAddr, ch.ID)
		mFile.WriteString(fmt.Sprintf("#EXTINF:-1 tvg-name=\"%s\" tvg-logo=\"%s\" group-title=\"%s\",%s\n%s\n",
			ch.Name, ch.Logo, ch.Group, ch.Name, proxyUrl))
	}

	// 2. 本地化图标并更新 Mapping (用于 index.html)
	fmt.Println("🖼️ 正在同步下载频道图标至本地...")
	var localMapping []Channel
	for _, ch := range validChannels {
		localCh := ch
		// 调用下载并更新路径
		localCh.Logo = downloadLogo(ch.ID, ch.Logo)
		localMapping = append(localMapping, localCh)
	}

	jsonPath := filepath.Join(outputDir, "mapping.json")
	jsonData, _ := json.MarshalIndent(localMapping, "", "  ")
	os.WriteFile(jsonPath, jsonData, 0644)

	fmt.Printf("🚀 全部处理完成！有效视频频道: %d 个，图标已存至 m3u/logos/\n", len(validChannels))
	return validChannels, nil
}
