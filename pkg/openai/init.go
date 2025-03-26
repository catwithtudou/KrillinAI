// Package openai 提供了 OpenAI API 的客户端封装
// 用于访问 OpenAI 的大语言模型服务，支持聊天补全等功能
// 支持自定义基础 URL、API 密钥和代理设置
package openai

import (
	"krillin-ai/config"
	"net/http"

	"github.com/sashabaranov/go-openai"
)

// Client 是 OpenAI API 的客户端封装
// 使用官方的 go-openai 库实现，提供对 OpenAI API 的访问
type Client struct {
	client *openai.Client // OpenAI 官方库的客户端实例
}

// NewClient 创建并初始化 OpenAI 客户端
// @param baseUrl OpenAI API 的基础 URL，为空时使用默认值
// @param apiKey OpenAI API 的访问密钥
// @param proxyAddr 代理服务器地址，为空时不使用代理
// @return *Client 初始化后的 OpenAI 客户端
func NewClient(baseUrl, apiKey, proxyAddr string) *Client {
	// 创建默认配置，设置 API 密钥
	cfg := openai.DefaultConfig(apiKey)
	if baseUrl != "" {
		// 如果提供了自定义 URL，则使用自定义 URL
		cfg.BaseURL = baseUrl
	}

	if proxyAddr != "" {
		// 如果提供了代理地址，则设置代理
		transport := &http.Transport{
			Proxy: http.ProxyURL(config.Conf.App.ParsedProxy),
		}
		cfg.HTTPClient = &http.Client{
			Transport: transport,
		}
	}

	// 使用配置创建 OpenAI 客户端
	client := openai.NewClientWithConfig(cfg)
	return &Client{client: client}
}
