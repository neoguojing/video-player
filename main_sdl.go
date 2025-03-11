//go:build sdl
// +build sdl

package main

import (
	"io"
	"net/http"
	"os"
	"time"
	"videoplayer/config"
	"videoplayer/server"

	"github.com/veandco/go-sdl2/sdl"

	_ "net/http/pprof"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	log "github.com/sirupsen/logrus"
)

func main() {
	config.GlobalConfig.UseOpenCV = false
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	setupLogger()

	// 创建 Server 实例
	svc := server.NewServer()

	// 设置路由
	svc.SetupRoutes()

	port := config.GlobalConfig.Port
	// 启动服务器
	go svc.Run(port)
	// 启动player
	sdl.Main(func() {
		svc.StartPlayer()
	})
	sdl.Quit()
}

func setupLogger() {
	// 设置日志级别
	level, err := log.ParseLevel(config.GlobalConfig.LogLevel)
	if err != nil {
		level = log.InfoLevel
	}
	log.SetLevel(level)
	log.SetReportCaller(true)

	// 设置将日志输出到文件
	filePath := "logs/video-player.log"
	writer, err := rotatelogs.New(
		filePath+".%Y%m%d",
		rotatelogs.WithLinkName(filePath),
		rotatelogs.WithMaxAge(time.Duration(14*24)*time.Hour),    // 保留14天
		rotatelogs.WithRotationTime(time.Duration(24)*time.Hour), // 每天分隔
	)
	if err != nil {
		log.Fatalf("failed to create rotatelogs: %v", err)
	}

	// 设置将日志输出到控制台及文件
	log.SetOutput(io.MultiWriter(os.Stdout, writer))

	// 设置日志格式
	log.SetFormatter(&log.TextFormatter{
		ForceColors:      false,
		FullTimestamp:    true,
		TimestampFormat:  time.RFC3339,
		DisableTimestamp: false,
	})
}
