package handler

import (
	"krillin-ai/internal/dto"
	"krillin-ai/internal/response"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

// StartSubtitleTask 启动字幕生成任务
// 该处理器负责接收字幕任务请求，验证参数，并启动异步处理流程
func (h Handler) StartSubtitleTask(c *gin.Context) {
	// 解析并验证请求参数
	var req dto.StartVideoSubtitleTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.R(c, response.Response{
			Error: -1,
			Msg:   "参数错误",
			Data:  nil,
		})
		return
	}

	// 获取服务实例，用于处理具体业务逻辑
	svc := h.Service

	// 调用服务层启动字幕任务
	data, err := svc.StartSubtitleTask(req)
	if err != nil {
		response.R(c, response.Response{
			Error: -1,
			Msg:   err.Error(),
			Data:  nil,
		})
		return
	}
	// 返回成功响应，包含任务ID等信息
	response.R(c, response.Response{
		Error: 0,
		Msg:   "成功",
		Data:  data,
	})
}

// GetSubtitleTask 获取字幕任务状态
// 该处理器用于查询字幕生成任务的进度和结果
func (h Handler) GetSubtitleTask(c *gin.Context) {
	// 解析并验证查询参数
	var req dto.GetVideoSubtitleTaskReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.R(c, response.Response{
			Error: -1,
			Msg:   "参数错误",
			Data:  nil,
		})
		return
	}
	// 获取服务实例
	svc := h.Service
	// 调用服务层获取任务状态
	data, err := svc.GetTaskStatus(req)
	if err != nil {
		response.R(c, response.Response{
			Error: -1,
			Msg:   err.Error(),
			Data:  nil,
		})
		return
	}
	// 返回任务状态信息
	response.R(c, response.Response{
		Error: 0,
		Msg:   "成功",
		Data:  data,
	})
}

// UploadFile 处理文件上传
// 该处理器负责接收上传的视频文件，并保存到本地存储
func (h Handler) UploadFile(c *gin.Context) {
	// 获取上传的文件
	file, err := c.FormFile("file")
	if err != nil {
		response.R(c, response.Response{
			Error: -1,
			Msg:   "未能获取文件",
			Data:  nil,
		})
		return
	}

	// 构建文件保存路径
	savePath := "./uploads/" + file.Filename
	// 保存上传的文件到本地
	if err = c.SaveUploadedFile(file, savePath); err != nil {
		response.R(c, response.Response{
			Error: -1,
			Msg:   "文件保存失败",
			Data:  nil,
		})
		return
	}

	// 返回成功响应，包含本地文件路径
	response.R(c, response.Response{
		Error: 0,
		Msg:   "文件上传成功",
		Data:  gin.H{"file_path": "local:" + savePath},
	})
}

// DownloadFile 处理文件下载
// 该处理器负责提供处理结果文件的下载功能
func (h Handler) DownloadFile(c *gin.Context) {
	// 获取请求的文件路径
	requestedFile := c.Param("filepath")
	if requestedFile == "" {
		response.R(c, response.Response{
			Error: -1,
			Msg:   "文件路径为空",
			Data:  nil,
		})
		return
	}

	// 构建本地文件路径
	localFilePath := filepath.Join(".", requestedFile)
	// 检查文件是否存在
	if _, err := os.Stat(localFilePath); os.IsNotExist(err) {
		response.R(c, response.Response{
			Error: -1,
			Msg:   "文件不存在",
			Data:  nil,
		})
		return
	}
	// 发送文件给客户端
	c.FileAttachment(localFilePath, filepath.Base(localFilePath))
}
