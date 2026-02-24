package parser

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Channel 表示一个电视频道的信息
type Channel struct {
	ID         string // 对原始URL进行MD5加密生成的唯一ID
	Name       string // 频道名称，从 #EXTINF 行提取
	Group      string // 分组名称，对应 group-title
	OriginalURL string // 原始TS流链接
	ProxyURL   string  // 本地生成的代理m3u8链接
}

// ParseAndRewrite 解析原始M3U文件并生成重写后的内容
// 参数:
//   - filePath: 原始M3U文件路径
//   - hostAddr: 本机地址(如 "127.0.0.1:15140")，用于生成代理URL
// 返回值:
//   - []Channel: 解析出的频道列表
//   - string: 重写后的M3U内容
//   - error: 解析错误时返回
func ParseAndRewrite(filePath, hostAddr string) ([]Channel, string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("打开文件失败: %v", err)
	}
	defer file.Close()

	var channels []Channel
	var output strings.Builder
	output.WriteString("#EXTM3U\n") // 写入M3U文件头

	scanner := bufio.NewScanner(file)
	var currentChannel *Channel

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#EXTINF:") {
			// 解析 #EXTINF 行
			name, group, err := parseExtinfLine(line)
			if err != nil {
				return nil, "", fmt.Errorf("解析EXTINF行失败: %v", err)
			}

			currentChannel = &Channel{
				Name:  name,
				Group: group,
			}
		} else if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") || strings.HasPrefix(line, "rtp://") {
			// 解析URL行
			if currentChannel == nil {
				continue
			}

			// 生成唯一ID (原始URL的MD5前16位)
			id := generateID(line)
			currentChannel.ID = id
			currentChannel.OriginalURL = line
			currentChannel.ProxyURL = fmt.Sprintf("http://%s/live/%s/index.m3u8", hostAddr, id)

			// 添加到频道列表
			channels = append(channels, *currentChannel)

			// 写入重写后的M3U内容
			output.WriteString(fmt.Sprintf("#EXTINF:-1 tvg-name=\"%s\" group-title=\"%s\", %s\n", 
				currentChannel.Name, currentChannel.Group, currentChannel.Name))
			output.WriteString(currentChannel.ProxyURL + "\n")

			currentChannel = nil
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, "", fmt.Errorf("读取文件失败: %v", err)
	}

	return channels, output.String(), nil
}

// parseExtinfLine 解析 #EXTINF 行，提取频道名和分组
// 示例行: #EXTINF:-1 tvg-id="CCTV1" tvg-name="CCTV-1" tvg-logo="CCTV1.png" group-title="央视",CCTV-1 综合
func parseExtinfLine(line string) (name, group string, err error) {
	// 提取 group-title
	groupRegex := regexp.MustCompile(`group-title="([^"]*)"`)
	if matches := groupRegex.FindStringSubmatch(line); len(matches) > 1 {
		group = matches[1]
	}

	// 提取 tvg-name
	nameRegex := regexp.MustCompile(`tvg-name="([^"]*)"`)
	if matches := nameRegex.FindStringSubmatch(line); len(matches) > 1 {
		name = matches[1]
	}

	// 如果 tvg-name 不存在，则取逗号后的内容作为频道名
	if name == "" {
		if lastComma := strings.LastIndex(line, ","); lastComma != -1 {
			name = strings.TrimSpace(line[lastComma+1:])
		}
	}

	// 如果都没有提取到，则使用默认值
	if name == "" {
		name = "未知频道"
	}
	if group == "" {
		group = "默认分组"
	}

	return name, group, nil
}

// generateID 生成URL的唯一ID (MD5前16位)
func generateID(url string) string {
	hash := md5.Sum([]byte(url))
	return hex.EncodeToString(hash[:])[:16]
}

// GetChannelByID 从频道列表中查找指定ID的频道
func GetChannelByID(channels []Channel, id string) *Channel {
	for _, ch := range channels {
		if ch.ID == id {
			return &ch
		}
	}
	return nil
}