package openai

import (
	"context"
	"io"
	"krillin-ai/config"
	"krillin-ai/log"

	openai "github.com/sashabaranov/go-openai"
	"go.uber.org/zap"
)

// ChatCompletion 使用 OpenAI 的聊天模型生成回复
// 使用流式API接收响应，适用于字幕翻译等需要较长输出的场景
// @param query 用户的查询内容或需要处理的文本
// @return string 模型生成的回复内容
// @return error 处理过程中的错误，如果有的话
func (c *Client) ChatCompletion(query string) (string, error) {
	// 构建聊天补全请求
	req := openai.ChatCompletionRequest{
		Model: openai.GPT4oMini20240718, // 默认使用 GPT-4o-mini 模型
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem, // 系统提示，定义AI助手的行为
				Content: "You are an assistant that helps with subtitle translation.",
			},
			{
				Role:    openai.ChatMessageRoleUser, // 用户消息
				Content: query,
			},
		},
		Stream:    true, // 启用流式响应，获取实时输出
		MaxTokens: 8192, // 最大输出标记数
	}

	// 如果配置中指定了模型，则使用配置中的模型
	if config.Conf.Openai.Model != "" {
		req.Model = config.Conf.Openai.Model
	}

	// 创建流式聊天补全请求
	stream, err := c.client.CreateChatCompletionStream(context.Background(), req)
	if err != nil {
		log.GetLogger().Error("openai create chat completion stream failed", zap.Error(err))
		return "", err
	}
	defer stream.Close() // 确保流在函数返回时关闭

	// 接收流式响应并拼接结果
	var resContent string
	for {
		// 从流中接收响应片段
		response, err := stream.Recv()
		if err == io.EOF {
			// 流结束
			break
		}
		if err != nil {
			// 接收中出现错误
			log.GetLogger().Error("openai stream receive failed", zap.Error(err))
			return "", err
		}

		// 累加响应内容
		resContent += response.Choices[0].Delta.Content
	}

	return resContent, nil
}
