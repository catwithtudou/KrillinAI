package service

import (
	"go.uber.org/zap"
	"krillin-ai/config"
	"krillin-ai/internal/types"
	"krillin-ai/log"
	"krillin-ai/pkg/aliyun"
	"krillin-ai/pkg/openai"
)

type Service struct {
	Transcriber      types.Transcriber
	ChatCompleter    types.ChatCompleter
	TtsClient        *aliyun.TtsClient
	OssClient        *aliyun.OssClient
	VoiceCloneClient *aliyun.VoiceCloneClient
}

func NewService() *Service {
	var transcriber types.Transcriber
	var chatCompleter types.ChatCompleter

	switch config.Conf.App.TranscribeProvider {
	case "openai":
		transcriber = openai.NewClient(config.Conf.Openai.ApiKey, config.Conf.App.Proxy)
	case "aliyun":
		transcriber = aliyun.NewAsrClient(config.Conf.Aliyun.Bailian.ApiKey)
	}
	log.GetLogger().Info("当前选择的转录源： ", zap.String("transcriber", config.Conf.App.TranscribeProvider))

	switch config.Conf.App.LlmProvider {
	case "openai":
		chatCompleter = openai.NewClient(config.Conf.Openai.ApiKey, config.Conf.App.Proxy)
	case "aliyun":
		chatCompleter = aliyun.NewChatClient(config.Conf.Aliyun.Bailian.ApiKey)
	}
	log.GetLogger().Info("当前选择的LLM源： ", zap.String("llm", config.Conf.App.LlmProvider))

	return &Service{
		Transcriber:      transcriber,
		ChatCompleter:    chatCompleter,
		TtsClient:        aliyun.NewTtsClient(config.Conf.Aliyun.Speech.AccessKeyId, config.Conf.Aliyun.Speech.AccessKeySecret, config.Conf.Aliyun.Speech.AppKey),
		OssClient:        aliyun.NewOssClient(config.Conf.Aliyun.Oss.AccessKeyId, config.Conf.Aliyun.Oss.AccessKeySecret, config.Conf.Aliyun.Oss.Bucket),
		VoiceCloneClient: aliyun.NewVoiceCloneClient(config.Conf.Aliyun.Speech.AccessKeyId, config.Conf.Aliyun.Speech.AccessKeySecret, config.Conf.Aliyun.Speech.AppKey),
	}
}
