package parser

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
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
	ID    string `json:"id"`
	Name  string `json:"name"`
	Logo  string `json:"logo"`
	Group string `json:"group"`
	Url   string `json:"url"`
}

// ValidateStream：探测并只保留 H.264 视频流
func ValidateStream(url string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second) // 缩短点超时
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-probesize", "32",
		"-analyzeduration", "0",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name",
		"-of", "csv=p=0",
		url)

	out, err := cmd.Output()
	if err != nil {
		return false
	}

	// 彻底清理不可见字符
	codec := strings.ToLower(strings.TrimSpace(string(out)))
	codec = strings.ReplaceAll(codec, "\n", "")
	codec = strings.ReplaceAll(codec, "\r", "")

	// 必须包含 h264 或 avc 且不能为空
	if codec != "" && (strings.Contains(codec, "h264") || strings.Contains(codec, "avc")) {
		return true
	}
	return false
}

func ParseAndGenerate(inputPath, serverAddr string) ([]Channel, error) {
	outputDir := "m3u"
	os.MkdirAll(outputDir, 0755)

	file, err := os.Open(inputPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var rawChannels []Channel
	scanner := bufio.NewScanner(file)

	// 正则表达式匹配
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

		// 1. 跳过 M3U 头部行
		if strings.HasPrefix(strings.ToUpper(line), "EXTM3U") || strings.HasPrefix(line, "#EXTM3U") {
			continue
		}

		// 2. 解析信息行
		if strings.HasPrefix(line, "#EXTINF:") {
			// 优先获取逗号后的显示名称
			if lastComma := strings.LastIndex(line, ","); lastComma != -1 {
				current.Name = line[lastComma+1:]
			}
			// 正则补充其他信息
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
			// 3. 处理地址行（必须以特定协议开头）
			lowerLine := strings.ToLower(line)

			// 严格准入：必须以协议开头，且不能是图片后缀
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
				current = Channel{} // 重置对象准备下一个
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
			if ValidateStream(c.Url) {
				mu.Lock()
				validChannels = append(validChannels, c)
				mu.Unlock()
				fmt.Printf("✅ 验证通过: %s\n", c.Name)
			}
			<-limit
		}(ch)
	}
	wg.Wait()

	// 排序
	sort.Slice(validChannels, func(i, j int) bool {
		return validChannels[i].ID < validChannels[j].ID
	})

	// 1. 生成订阅 m3u
	m3uPath := filepath.Join(outputDir, "tstohls.m3u")
	mFile, _ := os.Create(m3uPath)
	defer mFile.Close()
	mFile.WriteString("#EXTM3U\n")

	for _, ch := range validChannels {
		proxyUrl := fmt.Sprintf("%s/stream/%s/index.m3u8", serverAddr, ch.ID)
		mFile.WriteString(fmt.Sprintf("#EXTINF:-1 tvg-name=\"%s\" tvg-logo=\"%s\" group-title=\"%s\",%s\n%s\n",
			ch.Name, ch.Logo, ch.Group, ch.Name, proxyUrl))
	}

	// 2. 生成 mapping.json
	jsonPath := filepath.Join(outputDir, "mapping.json")
	jsonData, _ := json.MarshalIndent(validChannels, "", "  ")
	os.WriteFile(jsonPath, jsonData, 0644)

	fmt.Printf("🚀 全部处理完成！有效视频频道: %d 个\n", len(validChannels))
	return validChannels, nil
}
