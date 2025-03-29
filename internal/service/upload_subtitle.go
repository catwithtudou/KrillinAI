// Package service 提供核心业务逻辑服务实现
package service

import (
	"context"
	"fmt"
	"krillin-ai/internal/storage"
	"krillin-ai/internal/types"
	"krillin-ai/log"
	"krillin-ai/pkg/util"

	"go.uber.org/zap"
)

// uploadSubtitles 处理字幕上传的核心函数
// 该函数负责：
// 1. 处理字幕文件的替换操作（如果需要）
// 2. 生成字幕下载链接
// 3. 更新字幕任务状态
// 4. 处理配音文件的下载链接
//
// 参数：
//   - ctx: 上下文信息
//   - stepParam: 字幕任务步骤参数，包含任务ID、字幕信息、替换词映射等
//
// 返回：
//   - error: 处理过程中的错误信息
func (s Service) uploadSubtitles(ctx context.Context, stepParam *types.SubtitleTaskStepParam) error {
	// 初始化字幕信息切片
	subtitleInfos := make([]types.SubtitleInfo, 0)
	var err error

	// 遍历所有字幕信息进行处理
	for _, info := range stepParam.SubtitleInfos {
		resultPath := info.Path
		// 检查是否需要替换字幕内容
		if len(stepParam.ReplaceWordsMap) > 0 {
			// 生成替换后的文件路径
			replacedSrcFile := util.AddSuffixToFileName(resultPath, "_replaced")
			// 执行文件内容替换
			err = util.ReplaceFileContent(resultPath, replacedSrcFile, stepParam.ReplaceWordsMap)
			if err != nil {
				log.GetLogger().Error("uploadSubtitles ReplaceFileContent err", zap.Any("stepParam", stepParam), zap.Error(err))
				return fmt.Errorf("uploadSubtitles ReplaceFileContent err: %w", err)
			}
			// 更新结果文件路径为替换后的文件
			resultPath = replacedSrcFile
		}

		// 构建字幕信息并添加到结果列表
		subtitleInfos = append(subtitleInfos, types.SubtitleInfo{
			TaskId:      stepParam.TaskId,
			Name:        info.Name,
			DownloadUrl: "/api/file/" + resultPath,
		})
	}

	// 更新字幕任务状态信息
	storage.SubtitleTasks[stepParam.TaskId].SubtitleInfos = subtitleInfos
	storage.SubtitleTasks[stepParam.TaskId].Status = types.SubtitleTaskStatusSuccess
	storage.SubtitleTasks[stepParam.TaskId].ProcessPct = 100

	// 如果存在配音文件，更新配音文件的下载链接
	if stepParam.TtsResultFilePath != "" {
		storage.SubtitleTasks[stepParam.TaskId].SpeechDownloadUrl = "/api/file/" + stepParam.TtsResultFilePath
	}
	return nil
}
