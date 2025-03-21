package main

import (
	"fmt"
	"krillin-ai/config"
	"krillin-ai/internal/deps"
	"krillin-ai/internal/router"
	"krillin-ai/log"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// App 应用程序的主结构体，包含Web服务引擎
type App struct {
	Engine *gin.Engine // Gin HTTP框架的引擎实例
}

func main() {
	var err error
	// 初始化日志系统，使用zap作为日志库
	log.InitLogger()
	defer log.GetLogger().Sync() // 确保日志被正确写入

	// 加载应用配置，会从config.toml或环境变量中读取配置信息
	err = config.LoadConfig()
	if err != nil {
		log.GetLogger().Error("加载配置失败", zap.Error(err))
		return
	}

	// 检查并准备运行环境依赖（如ffmpeg、ffprobe、yt-dlp等）
	// 如果配置了本地转写服务，还会检查fasterwhisper和模型文件
	err = deps.CheckDependency()
	if err != nil {
		log.GetLogger().Error("依赖环境准备失败", zap.Error(err))
		return
	}

	// 设置Gin为生产模式，减少调试信息输出
	gin.SetMode(gin.ReleaseMode)
	// 创建应用实例
	app := App{
		Engine: gin.Default(), // 创建默认的Gin引擎，包含Logger和Recovery中间件
	}

	// 设置API路由，包括字幕任务、文件上传下载等接口
	router.SetupRouter(app.Engine)
	// 记录服务启动日志
	log.GetLogger().Info("服务启动", zap.String("host", config.Conf.Server.Host), zap.Int("port", config.Conf.Server.Port))
	// 启动HTTP服务器
	_ = app.Engine.Run(fmt.Sprintf("%s:%d", config.Conf.Server.Host, config.Conf.Server.Port))
}
