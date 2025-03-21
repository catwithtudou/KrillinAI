package config

import (
	"errors"         // 用于创建和返回错误
	"krillin-ai/log" // 导入项目自定义日志包
	"net/url"        // 用于解析和处理URL
	"os"             // 提供操作系统功能，如文件访问和环境变量
	"strconv"        // 提供字符串转换功能

	"github.com/BurntSushi/toml" // 用于解析TOML格式的配置文件
)

// App 应用程序核心配置结构体
type App struct {
	SegmentDuration      int      `toml:"segment_duration"`       // 音频分段时长（秒），用于音频处理
	TranslateParallelNum int      `toml:"translate_parallel_num"` // 翻译并行处理的数量，控制并发
	Proxy                string   `toml:"proxy"`                  // 代理服务器地址，用于网络请求
	ParsedProxy          *url.URL // 解析后的代理URL对象，不保存到配置文件
	TranscribeProvider   string   `toml:"transcribe_provider"` // 转写服务提供商（openai/fasterwhisper/aliyun）
	LlmProvider          string   `toml:"llm_provider"`        // 大语言模型提供商（openai/aliyun）
}

// Server Web服务器配置结构体
type Server struct {
	Host string `toml:"host"` // 服务器监听的主机地址
	Port int    `toml:"port"` // 服务器监听的端口号
}

// LocalModel 本地模型配置结构体
type LocalModel struct {
	FasterWhisper string `toml:"faster_whisper"` // FasterWhisper模型大小（tiny/medium/large-v2）
}

// Openai OpenAI服务配置结构体
type Openai struct {
	BaseUrl string `toml:"base_url"` // OpenAI API的基础URL，支持自定义或第三方兼容接口
	Model   string `toml:"model"`    // 使用的模型名称
	ApiKey  string `toml:"api_key"`  // OpenAI的API密钥
}

// AliyunOss 阿里云对象存储服务配置
type AliyunOss struct {
	AccessKeyId     string `toml:"access_key_id"`     // 阿里云访问ID
	AccessKeySecret string `toml:"access_key_secret"` // 阿里云访问密钥
	Bucket          string `toml:"bucket"`            // OSS存储桶名称
}

// AliyunSpeech 阿里云语音服务配置
type AliyunSpeech struct {
	AccessKeyId     string `toml:"access_key_id"`     // 阿里云访问ID
	AccessKeySecret string `toml:"access_key_secret"` // 阿里云访问密钥
	AppKey          string `toml:"app_key"`           // 阿里云语音服务的应用密钥
}

// AliyunBailian 阿里云百炼大语言模型服务配置
type AliyunBailian struct {
	ApiKey string `toml:"api_key"` // 阿里云百炼服务API密钥
}

// Aliyun 阿里云服务总配置结构体
type Aliyun struct {
	Oss     AliyunOss     `toml:"oss"`     // 阿里云对象存储配置
	Speech  AliyunSpeech  `toml:"speech"`  // 阿里云语音服务配置
	Bailian AliyunBailian `toml:"bailian"` // 阿里云百炼大模型配置
}

// Config 全局配置结构体，整合所有配置模块
type Config struct {
	App        App        `toml:"app"`         // 应用核心配置
	Server     Server     `toml:"server"`      // 服务器配置
	LocalModel LocalModel `toml:"local_model"` // 本地模型配置
	Openai     Openai     `toml:"openai"`      // OpenAI服务配置
	Aliyun     Aliyun     `toml:"aliyun"`      // 阿里云服务配置
}

// Conf 全局配置实例，包含默认值
// 这些默认值在没有配置文件且环境变量未设置时使用
var Conf = Config{
	App: App{
		SegmentDuration:      5,        // 默认音频分段为5秒
		TranslateParallelNum: 5,        // 默认5个并发翻译任务
		TranscribeProvider:   "openai", // 默认使用OpenAI作为转写提供商
		LlmProvider:          "openai", // 默认使用OpenAI作为LLM提供商
	},
	Server: Server{
		Host: "127.0.0.1", // 默认监听本地回环地址
		Port: 8888,        // 默认端口8888
	},
	LocalModel: LocalModel{
		FasterWhisper: "medium", // 默认使用中等大小的FasterWhisper模型
	},
}

// loadFromEnv 从环境变量加载配置
// 允许通过环境变量覆盖默认配置和配置文件中的设置
func loadFromEnv() {
	// App 配置
	if v := os.Getenv("KRILLIN_SEGMENT_DURATION"); v != "" {
		if duration, err := strconv.Atoi(v); err == nil {
			Conf.App.SegmentDuration = duration
		}
	}
	if v := os.Getenv("KRILLIN_TRANSLATE_PARALLEL_NUM"); v != "" {
		if num, err := strconv.Atoi(v); err == nil {
			Conf.App.TranslateParallelNum = num
		}
	}
	if v := os.Getenv("KRILLIN_PROXY"); v != "" {
		Conf.App.Proxy = v
	}
	if v := os.Getenv("KRILLIN_TRANSCRIBE_PROVIDER"); v != "" {
		Conf.App.TranscribeProvider = v
	}
	if v := os.Getenv("KRILLIN_LLM_PROVIDER"); v != "" {
		Conf.App.LlmProvider = v
	}

	// Server 配置
	if v := os.Getenv("KRILLIN_SERVER_HOST"); v != "" {
		Conf.Server.Host = v
	}
	if v := os.Getenv("KRILLIN_SERVER_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			Conf.Server.Port = port
		}
	}

	// LocalModel 配置
	if v := os.Getenv("KRILLIN_FASTER_WHISPER"); v != "" {
		Conf.LocalModel.FasterWhisper = v
	}

	// OpenAI 配置
	if v := os.Getenv("KRILLIN_OPENAI_BASE_URL"); v != "" {
		Conf.Openai.BaseUrl = v
	}
	if v := os.Getenv("KRILLIN_OPENAI_MODEL"); v != "" {
		Conf.Openai.Model = v
	}
	if v := os.Getenv("KRILLIN_OPENAI_API_KEY"); v != "" {
		Conf.Openai.ApiKey = v
	}

	// Aliyun OSS 配置
	if v := os.Getenv("KRILLIN_ALIYUN_OSS_ACCESS_KEY_ID"); v != "" {
		Conf.Aliyun.Oss.AccessKeyId = v
	}
	if v := os.Getenv("KRILLIN_ALIYUN_OSS_ACCESS_KEY_SECRET"); v != "" {
		Conf.Aliyun.Oss.AccessKeySecret = v
	}
	if v := os.Getenv("KRILLIN_ALIYUN_OSS_BUCKET"); v != "" {
		Conf.Aliyun.Oss.Bucket = v
	}

	// Aliyun Speech 配置
	if v := os.Getenv("KRILLIN_ALIYUN_SPEECH_ACCESS_KEY_ID"); v != "" {
		Conf.Aliyun.Speech.AccessKeyId = v
	}
	if v := os.Getenv("KRILLIN_ALIYUN_SPEECH_ACCESS_KEY_SECRET"); v != "" {
		Conf.Aliyun.Speech.AccessKeySecret = v
	}
	if v := os.Getenv("KRILLIN_ALIYUN_SPEECH_APP_KEY"); v != "" {
		Conf.Aliyun.Speech.AppKey = v
	}

	// Aliyun Bailian 配置
	if v := os.Getenv("KRILLIN_ALIYUN_BAILIAN_API_KEY"); v != "" {
		Conf.Aliyun.Bailian.ApiKey = v
	}
}

// validateConfig 验证配置的有效性和完整性
// 确保所选服务提供商的必要配置已正确设置
func validateConfig() error {
	// 检查转写服务提供商配置
	switch Conf.App.TranscribeProvider {
	case "openai":
		// OpenAI转写服务需要API密钥
		if Conf.Openai.ApiKey == "" {
			return errors.New("使用OpenAI转写服务需要配置 OpenAI API Key")
		}
	case "fasterwhisper":
		// 验证FasterWhisper模型选择是否有效
		if Conf.LocalModel.FasterWhisper != "tiny" && Conf.LocalModel.FasterWhisper != "medium" && Conf.LocalModel.FasterWhisper != "large-v2" {
			return errors.New("检测到开启了fasterwhisper，但模型选型配置不正确，请检查配置")
		}
	case "aliyun":
		// 阿里云语音服务需要完整的密钥配置
		if Conf.Aliyun.Speech.AccessKeyId == "" || Conf.Aliyun.Speech.AccessKeySecret == "" || Conf.Aliyun.Speech.AppKey == "" {
			return errors.New("使用阿里云语音服务需要配置相关密钥")
		}
	default:
		return errors.New("不支持的转录提供商")
	}

	// 检查LLM提供商配置
	switch Conf.App.LlmProvider {
	case "openai":
		// OpenAI LLM服务需要API密钥
		if Conf.Openai.ApiKey == "" {
			return errors.New("使用OpenAI LLM服务需要配置 OpenAI API Key")
		}
	case "aliyun":
		// 阿里云百炼服务需要API密钥
		if Conf.Aliyun.Bailian.ApiKey == "" {
			return errors.New("使用阿里云百炼服务需要配置 API Key")
		}
	default:
		return errors.New("不支持的LLM提供商")
	}

	return nil
}

// LoadConfig 加载配置的主函数
// 按照优先级依次尝试：配置文件 -> 环境变量 -> 默认值
func LoadConfig() error {
	var err error
	configPath := "./config/config.toml"

	// 检查配置文件是否存在
	if _, err = os.Stat(configPath); os.IsNotExist(err) {
		// 配置文件不存在，从环境变量加载
		log.GetLogger().Info("未找到配置文件，从环境变量中加载配置")
		loadFromEnv()
	} else {
		// 配置文件存在，优先从配置文件加载
		log.GetLogger().Info("已找到配置文件，从配置文件中加载配置")
		_, err = toml.DecodeFile(configPath, &Conf)
	}

	// 解析代理地址（如果设置了代理）
	Conf.App.ParsedProxy, err = url.Parse(Conf.App.Proxy)
	if err != nil {
		return err
	}

	// 使用本地FasterWhisper模型时不允许并行处理（资源限制）
	if Conf.App.TranscribeProvider == "fasterwhisper" {
		Conf.App.TranslateParallelNum = 1
	}

	// 验证配置是否完整有效
	return validateConfig()
}
