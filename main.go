package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"TsToHls/manager"
	"TsToHls/parser"

	"github.com/gin-gonic/gin"
)

const (
	Port         = "15140"      // 服务监听端口
	HLSOutputDir = "./data/hls" // HLS输出目录
	M3UInputDir  = "./data/m3u" // 上传的M3U文件目录
)

// 全局变量
var (
	pm            *manager.ProcessManager // FFmpeg进程管理器
	channelList   []parser.Channel        // 解析后的频道列表
)

func main() {
	// 初始化
	pm = manager.NewProcessManager()
	cleanOutputDirs()

	// 创建Gin引擎
	r := gin.Default()

	// 静态文件路由
	r.Static("/web", "./web")        // 前端页面
	r.Static("/live", HLSOutputDir)  // HLS切片文件

	// API路由
	api := r.Group("/api")
	{
		api.POST("/upload", handleUpload)      // 上传M3U文件
		api.GET("/status", handleStatus)       // 获取服务状态
	}

	// 代理路由
	r.GET("/stream/:id/index.m3u8", handleStreamRequest)

	// 启动服务
	fmt.Printf("服务启动，监听端口 %s\n", Port)
	r.Run(":" + Port)
}

// cleanOutputDirs 清理输出目录
func cleanOutputDirs() {
	os.RemoveAll(HLSOutputDir)
	os.MkdirAll(HLSOutputDir, 0755)
	os.MkdirAll(M3UInputDir, 0755)
}

// handleUpload 处理M3U文件上传
func handleUpload(c *gin.Context) {
	file, err := c.FormFile("m3uFile")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的上传文件"})
		return
	}

	// 保存上传的文件
	m3uPath := filepath.Join(M3UInputDir, file.Filename)
	if err := c.SaveUploadedFile(file, m3uPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "文件保存失败"})
		return
	}

	// 解析并重写M3U文件
	channels, newM3UContent, err := parser.ParseAndRewrite(m3uPath, "localhost:"+Port)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "解析M3U文件失败: " + err.Error()})
		return
	}

	// 更新全局频道列表
	channelList = channels

	// 返回结果
	c.JSON(http.StatusOK, gin.H{
		"channels": channels,
		"m3u":      newM3UContent,
	})
}

// handleStreamRequest 处理流请求
// 访问格式: /stream/[channelID]/index.m3u8
func handleStreamRequest(c *gin.Context) {
	id := c.Param("id")
	channel := parser.GetChannelByID(channelList, id)
	if channel == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "频道不存在"})
		return
	}

	// 准备输出目录
	outputDir := filepath.Join(HLSOutputDir, id)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建输出目录失败"})
		return
	}

	// 启动FFmpeg转码进程
	if err := pm.StartProcess(id, channel.OriginalURL, outputDir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "启动转码进程失败: " + err.Error()})
		return
	}

	// 等待HLS文件生成（最多10秒）
	m3u8Path := filepath.Join(outputDir, "index.m3u8")
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(m3u8Path); err == nil {
			// 文件已生成，重定向到静态文件
			c.Redirect(http.StatusFound, "/live/"+id+"/index.m3u8")
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	c.JSON(http.StatusInternalServerError, gin.H{"error": "HLS文件生成超时"})
}

// handleStatus 获取服务状态
func handleStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"processCount": pm.GetProcessCount(),
		"channelCount": len(channelList),
	})
}