package service

import (
	"context"
	"errors"
	"fmt"
	"krillin-ai/internal/dto"
	"krillin-ai/internal/storage"
	"krillin-ai/internal/types"
	"krillin-ai/log"
	"krillin-ai/pkg/util"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/samber/lo"
	"go.uber.org/zap"
)

// StartSubtitleTask 启动字幕生成任务的核心服务方法
// 该方法负责初始化任务参数、创建任务目录、启动异步处理流程
func (s Service) StartSubtitleTask(req dto.StartVideoSubtitleTaskReq) (*dto.StartVideoSubtitleTaskResData, error) {
	// 1. 视频链接验证
	// 检查YouTube链接
	if strings.Contains(req.Url, "youtube.com") {
		videoId, _ := util.GetYouTubeID(req.Url)
		if videoId == "" {
			return nil, fmt.Errorf("链接不合法")
		}
	}
	// 检查Bilibili链接
	if strings.Contains(req.Url, "bilibili.com") {
		videoId := util.GetBilibiliVideoId(req.Url)
		if videoId == "" {
			return nil, fmt.Errorf("链接不合法")
		}
	}

	// 2. 任务初始化
	// 生成唯一任务ID
	taskId := util.GenerateRandStringWithUpperLowerNum(8)

	// 3. 字幕类型确定
	var resultType types.SubtitleResultType
	// 根据用户配置确定字幕显示类型
	if req.TargetLang == "none" {
		resultType = types.SubtitleResultTypeOriginOnly
	} else {
		if req.Bilingual == types.SubtitleTaskBilingualYes {
			if req.TranslationSubtitlePos == types.SubtitleTaskTranslationSubtitlePosTop {
				resultType = types.SubtitleResultTypeBilingualTranslationOnTop
			} else {
				resultType = types.SubtitleResultTypeBilingualTranslationOnBottom
			}
		} else {
			resultType = types.SubtitleResultTypeTargetOnly
		}
	}

	// 4. 文字替换规则处理
	replaceWordsMap := make(map[string]string)
	if len(req.Replace) > 0 {
		for _, replace := range req.Replace {
			beforeAfter := strings.Split(replace, "|")
			if len(beforeAfter) == 2 {
				replaceWordsMap[beforeAfter[0]] = beforeAfter[1]
			} else {
				log.GetLogger().Info("generateAudioSubtitles replace param length err", zap.Any("replace", replace), zap.Any("taskId", taskId))
			}
		}
	}

	// 5. 任务目录创建
	var err error
	ctx := context.Background()
	taskBasePath := filepath.Join("./tasks", taskId)
	if _, err = os.Stat(taskBasePath); os.IsNotExist(err) {
		err = os.MkdirAll(filepath.Join(taskBasePath, "output"), os.ModePerm)
		if err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask MkdirAll err", zap.Any("req", req), zap.Error(err))
		}
	}

	// 6. 任务状态初始化
	storage.SubtitleTasks[taskId] = &types.SubtitleTask{
		TaskId:   taskId,
		VideoSrc: req.Url,
		Status:   types.SubtitleTaskStatusProcessing,
	}

	// 7. TTS语音配置
	var ttsVoiceCode string
	if req.TtsVoiceCode == types.SubtitleTaskTtsVoiceCodeLongyu {
		ttsVoiceCode = "longyu"
	} else {
		ttsVoiceCode = "longchen"
	}

	// 8. 声音克隆处理
	var voiceCloneAudioUrl string
	if req.TtsVoiceCloneSrcFileUrl != "" {
		localFileUrl := strings.TrimPrefix(req.TtsVoiceCloneSrcFileUrl, "local:")
		fileKey := util.GenerateRandStringWithUpperLowerNum(5) + filepath.Ext(localFileUrl)
		err = s.OssClient.UploadFile(context.Background(), fileKey, localFileUrl, s.OssClient.Bucket)
		if err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask UploadFile err", zap.Any("req", req), zap.Error(err))
			return nil, errors.New("上传声音克隆源失败")
		}
		voiceCloneAudioUrl = fmt.Sprintf("https://%s.oss-cn-shanghai.aliyuncs.com/%s", s.OssClient.Bucket, fileKey)
		log.GetLogger().Info("StartVideoSubtitleTask 上传声音克隆源成功", zap.Any("oss url", voiceCloneAudioUrl))
	}

	// 9. 任务参数构建
	stepParam := types.SubtitleTaskStepParam{
		TaskId:                  taskId,
		TaskBasePath:            taskBasePath,
		Link:                    req.Url,
		SubtitleResultType:      resultType,
		EnableModalFilter:       req.ModalFilter == types.SubtitleTaskModalFilterYes,
		EnableTts:               req.Tts == types.SubtitleTaskTtsYes,
		TtsVoiceCode:            ttsVoiceCode,
		VoiceCloneAudioUrl:      voiceCloneAudioUrl,
		ReplaceWordsMap:         replaceWordsMap,
		OriginLanguage:          types.StandardLanguageName(req.OriginLanguage),
		TargetLanguage:          types.StandardLanguageName(req.TargetLang),
		UserUILanguage:          types.StandardLanguageName(req.Language),
		EmbedSubtitleVideoType:  req.EmbedSubtitleVideoType,
		VerticalVideoMajorTitle: req.VerticalMajorTitle,
		VerticalVideoMinorTitle: req.VerticalMinorTitle,
		MaxWordOneLine:          12, // 默认每行最大字数
	}
	if req.OriginLanguageWordOneLine != 0 {
		stepParam.MaxWordOneLine = req.OriginLanguageWordOneLine
	}

	// 10. 启动异步处理流程
	go func() {
		// 异常恢复处理
		defer func() {
			if r := recover(); r != nil {
				const size = 64 << 10
				buf := make([]byte, size)
				buf = buf[:runtime.Stack(buf, false)]
				log.GetLogger().Error("autoVideoSubtitle panic", zap.Any("panic:", r), zap.Any("stack:", buf))
				storage.SubtitleTasks[taskId].Status = types.SubtitleTaskStatusFailed
			}
		}()

		// 执行任务处理流程
		log.GetLogger().Info("video subtitle start task", zap.String("taskId", taskId))

		// 10.1 下载视频/音频文件
		err = s.linkToFile(ctx, &stepParam)
		if err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask linkToFile err", zap.Any("req", req), zap.Error(err))
			storage.SubtitleTasks[stepParam.TaskId].Status = types.SubtitleTaskStatusFailed
			storage.SubtitleTasks[stepParam.TaskId].FailReason = err.Error()
			return
		}

		// 10.2 音频转字幕
		err = s.audioToSubtitle(ctx, &stepParam)
		if err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask audioToSubtitle err", zap.Any("req", req), zap.Error(err))
			storage.SubtitleTasks[stepParam.TaskId].Status = types.SubtitleTaskStatusFailed
			storage.SubtitleTasks[stepParam.TaskId].FailReason = err.Error()
			return
		}

		// 10.3 字幕转语音
		err = s.srtFileToSpeech(ctx, &stepParam)
		if err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask srtFileToSpeech err", zap.Any("req", req), zap.Error(err))
			storage.SubtitleTasks[stepParam.TaskId].Status = types.SubtitleTaskStatusFailed
			storage.SubtitleTasks[stepParam.TaskId].FailReason = err.Error()
			return
		}

		// 10.4 嵌入字幕到视频
		err = s.embedSubtitles(ctx, &stepParam)
		if err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask embedSubtitles err", zap.Any("req", req), zap.Error(err))
			storage.SubtitleTasks[stepParam.TaskId].Status = types.SubtitleTaskStatusFailed
			storage.SubtitleTasks[stepParam.TaskId].FailReason = err.Error()
			return
		}

		// 10.5 上传处理结果
		err = s.uploadSubtitles(ctx, &stepParam)
		if err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask uploadSubtitles err", zap.Any("req", req), zap.Error(err))
			storage.SubtitleTasks[stepParam.TaskId].Status = types.SubtitleTaskStatusFailed
			storage.SubtitleTasks[stepParam.TaskId].FailReason = err.Error()
			return
		}

		log.GetLogger().Info("video subtitle task end", zap.String("taskId", taskId))
	}()

	// 11. 返回任务ID
	return &dto.StartVideoSubtitleTaskResData{
		TaskId: taskId,
	}, nil
}

// GetTaskStatus 获取字幕任务状态的服务方法
// 该方法负责查询任务进度、状态和结果信息
func (s Service) GetTaskStatus(req dto.GetVideoSubtitleTaskReq) (*dto.GetVideoSubtitleTaskResData, error) {
	// 1. 获取任务信息
	task := storage.SubtitleTasks[req.TaskId]
	if task == nil {
		return nil, errors.New("任务不存在")
	}
	// 2. 检查任务状态
	if task.Status == types.SubtitleTaskStatusFailed {
		return nil, fmt.Errorf("任务失败，原因：%s", task.FailReason)
	}
	// 3. 构建返回数据
	return &dto.GetVideoSubtitleTaskResData{
		TaskId:         task.TaskId,
		ProcessPercent: task.ProcessPct,
		VideoInfo: &dto.VideoInfo{
			Title:                 task.Title,
			Description:           task.Description,
			TranslatedTitle:       task.TranslatedTitle,
			TranslatedDescription: task.TranslatedDescription,
		},
		SubtitleInfo: lo.Map(task.SubtitleInfos, func(item types.SubtitleInfo, _ int) *dto.SubtitleInfo {
			return &dto.SubtitleInfo{
				Name:        item.Name,
				DownloadUrl: item.DownloadUrl,
			}
		}),
		TargetLanguage:    task.TargetLanguage,
		SpeechDownloadUrl: task.SpeechDownloadUrl,
	}, nil
}
