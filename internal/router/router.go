package router

import (
	"krillin-ai/internal/handler"
	"krillin-ai/static"
	"net/http"

	"github.com/gin-gonic/gin"
)

// SetupRouter 配置并初始化Gin路由引擎
// 该函数负责设置所有的HTTP路由规则，包括API接口和静态文件服务
func SetupRouter(r *gin.Engine) {
	// 创建API路由组，所有API请求都以/api为前缀
	api := r.Group("/api")

	// 初始化处理器，用于处理具体的业务逻辑
	hdl := handler.NewHandler()
	{
		// 字幕任务相关接口
		// POST /api/capability/subtitleTask - 启动新的字幕生成任务
		api.POST("/capability/subtitleTask", hdl.StartSubtitleTask)
		// GET /api/capability/subtitleTask - 获取字幕任务的状态和结果
		api.GET("/capability//subtitleTask", hdl.GetSubtitleTask)

		// 文件处理相关接口
		// POST /api/file - 上传视频文件
		api.POST("/file", hdl.UploadFile)
		// GET /api/file/*filepath - 下载处理后的文件，支持任意路径
		api.GET("/file/*filepath", hdl.DownloadFile)
	}

	// 根路径重定向到静态文件目录
	// 当访问根路径/时，自动重定向到/static目录
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/static")
	})

	// 设置静态文件服务
	// 使用embed包嵌入的静态文件，提供前端界面访问
	// 所有/static/*的请求都会从嵌入的文件系统中获取对应的静态资源
	r.StaticFS("/static", http.FS(static.EmbeddedFiles))
}
