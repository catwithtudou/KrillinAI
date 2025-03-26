// Package service 实现了应用程序的核心服务层
// init.go 负责初始化服务层的各个组件，包括语音识别、对话生成、语音合成等服务
// 支持多种服务提供商的灵活配置，如OpenAI、阿里云等
package service

import (
	"krillin-ai/config"
	"krillin-ai/internal/types"
	"krillin-ai/log"
	"krillin-ai/pkg/aliyun"
	"krillin-ai/pkg/fasterwhisper"
	"krillin-ai/pkg/openai"
	"krillin-ai/pkg/whisper"
	"krillin-ai/pkg/whisperkit"

	"go.uber.org/zap"
)

// Service 是应用的核心服务结构体，集成了所有功能模块的客户端
type Service struct {
	Transcriber      types.Transcriber        // 语音识别服务，将音频转换为文本
	ChatCompleter    types.ChatCompleter      // 对话生成服务，用于文本翻译和处理
	TtsClient        *aliyun.TtsClient        // 文本转语音服务，用于生成语音
	OssClient        *aliyun.OssClient        // 对象存储服务，用于存储音频和字幕文件
	VoiceCloneClient *aliyun.VoiceCloneClient // 声音克隆服务，用于个性化语音合成
}

// NewService 创建并初始化服务实例
// 根据配置文件选择适当的服务提供商，并初始化相应的客户端
// 支持多种语音识别和大语言模型提供商，如OpenAI、阿里云、本地模型等
// @return *Service 初始化后的服务实例
func NewService() *Service {
	var transcriber types.Transcriber
	var chatCompleter types.ChatCompleter

	// 根据配置选择语音识别服务提供商
	switch config.Conf.App.TranscribeProvider {
	case "openai":
		// 使用OpenAI的Whisper服务进行语音识别
		transcriber = whisper.NewClient(config.Conf.Openai.Whisper.BaseUrl, config.Conf.Openai.Whisper.ApiKey, config.Conf.App.Proxy)
	case "aliyun":
		// 使用阿里云的语音识别服务
		transcriber = aliyun.NewAsrClient(config.Conf.Aliyun.Bailian.ApiKey)
	case "fasterwhisper":
		// 使用本地部署的FasterWhisper模型
		transcriber = fasterwhisper.NewFastwhisperProcessor(config.Conf.LocalModel.Whisper)
	case "whisperkit":
		// 使用WhisperKit处理器（可能是针对特定平台优化的版本）
		transcriber = whisperkit.NewWhisperKitProcessor(config.Conf.LocalModel.Whisper)
	}
	log.GetLogger().Info("当前选择的转录源： ", zap.String("transcriber", config.Conf.App.TranscribeProvider))

	// 根据配置选择大语言模型提供商
	switch config.Conf.App.LlmProvider {
	case "openai":
		// 使用OpenAI的大语言模型服务
		chatCompleter = openai.NewClient(config.Conf.Openai.BaseUrl, config.Conf.Openai.ApiKey, config.Conf.App.Proxy)
	case "aliyun":
		// 使用阿里云的大语言模型服务（百炼）
		chatCompleter = aliyun.NewChatClient(config.Conf.Aliyun.Bailian.ApiKey)
	}
	log.GetLogger().Info("当前选择的LLM源： ", zap.String("llm", config.Conf.App.LlmProvider))

	// 创建服务实例，集成所有功能模块
	return &Service{
		Transcriber:      transcriber,                                                                                                                                    // 语音识别服务
		ChatCompleter:    chatCompleter,                                                                                                                                  // 对话生成服务
		TtsClient:        aliyun.NewTtsClient(config.Conf.Aliyun.Speech.AccessKeyId, config.Conf.Aliyun.Speech.AccessKeySecret, config.Conf.Aliyun.Speech.AppKey),        // 阿里云语音合成服务
		OssClient:        aliyun.NewOssClient(config.Conf.Aliyun.Oss.AccessKeyId, config.Conf.Aliyun.Oss.AccessKeySecret, config.Conf.Aliyun.Oss.Bucket),                 // 阿里云对象存储服务
		VoiceCloneClient: aliyun.NewVoiceCloneClient(config.Conf.Aliyun.Speech.AccessKeyId, config.Conf.Aliyun.Speech.AccessKeySecret, config.Conf.Aliyun.Speech.AppKey), // 阿里云声音克隆服务
	}
}
