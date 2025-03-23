package deps

import (
	"fmt"
	"krillin-ai/config"
	"krillin-ai/internal/storage"
	"krillin-ai/log"
	"krillin-ai/pkg/util"
	"os"
	"os/exec"
	"runtime"

	"go.uber.org/zap"
)

// CheckDependency 检查并准备项目运行所需的所有依赖
// 包括：ffmpeg、ffprobe、yt-dlp等工具以及相关模型
// 根据配置的转写提供商不同，会检查不同的模型环境
func CheckDependency() error {
	// 检查并下载ffmpeg，用于视频处理
	err := checkAndDownloadFfmpeg()
	if err != nil {
		log.GetLogger().Error("ffmpeg环境准备失败", zap.Error(err))
		return err
	}
	// 检查并下载ffprobe，用于媒体文件分析
	err = checkAndDownloadFfprobe()
	if err != nil {
		log.GetLogger().Error("ffprobe环境准备失败", zap.Error(err))
		return err
	}
	// 检查并下载yt-dlp，用于从YouTube等平台下载视频
	err = checkAndDownloadYtDlp()
	if err != nil {
		log.GetLogger().Error("yt-dlp环境准备失败", zap.Error(err))
		return err
	}
	// 当配置使用fasterwhisper作为转写提供商时
	if config.Conf.App.TranscribeProvider == "fasterwhisper" {
		// 检查fasterwhisper环境
		err = checkFasterWhisper()
		if err != nil {
			log.GetLogger().Error("fasterwhisper环境准备失败", zap.Error(err))
			return err
		}
		// 检查并下载所需的模型文件
		err = checkModel("fasterwhisper")
		if err != nil {
			log.GetLogger().Error("本地模型环境准备失败", zap.Error(err))
			return err
		}
	}
	// 当配置使用whisperkit作为转写提供商时（仅支持macOS设备）
	if config.Conf.App.TranscribeProvider == "whisperkit" {
		if err = checkWhisperKit(); err != nil {
			log.GetLogger().Error("whisperkit环境准备失败", zap.Error(err))
			return err
		}
		// 检查并下载所需的模型文件
		err = checkModel("whisperkit")
		if err != nil {
			log.GetLogger().Error("本地模型环境准备失败", zap.Error(err))
			return err
		}
	}

	return nil
}

// checkAndDownloadFfmpeg 检测并安装ffmpeg
// 如果系统中已经安装了ffmpeg，则直接使用
// 否则会自动下载适合当前操作系统的版本并解压到./bin目录
func checkAndDownloadFfmpeg() error {
	// 检查系统环境变量中是否已经有ffmpeg
	_, err := exec.LookPath("ffmpeg")
	if err == nil {
		log.GetLogger().Info("已找到ffmpeg")
		// 设置全局变量，供其他模块使用
		storage.FfmpegPath = "ffmpeg"
		return nil
	}

	// 构建本地bin目录中ffmpeg的路径
	ffmpegBinFilePath := "./bin/ffmpeg"
	if runtime.GOOS == "windows" {
		ffmpegBinFilePath += ".exe"
	}
	// 检查之前是否已经下载过ffmpeg
	if _, err = os.Stat(ffmpegBinFilePath); err == nil {
		log.GetLogger().Info("已找到ffmpeg")
		storage.FfmpegPath = ffmpegBinFilePath
		return nil
	}

	log.GetLogger().Info("没有找到ffmpeg，即将开始自动安装")
	// 确保./bin目录存在
	err = os.MkdirAll("./bin", 0755)
	if err != nil {
		log.GetLogger().Error("创建./bin目录失败", zap.Error(err))
		return err
	}

	// 根据不同操作系统选择对应的下载链接
	var ffmpegURL string
	if runtime.GOOS == "linux" {
		ffmpegURL = "https://modelscope.cn/models/Maranello/KrillinAI_dependency_cn/resolve/master/ffmpeg-6.1-linux-64.zip"
	} else if runtime.GOOS == "darwin" {
		ffmpegURL = "https://modelscope.cn/models/Maranello/KrillinAI_dependency_cn/resolve/master/ffmpeg-6.1-macos-64.zip"
	} else if runtime.GOOS == "windows" {
		ffmpegURL = "https://modelscope.cn/models/Maranello/KrillinAI_dependency_cn/resolve/master/ffmpeg-6.1-win-64.zip"
	} else {
		log.GetLogger().Error("不支持你当前的操作系统", zap.String("当前系统", runtime.GOOS))
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	// 下载ffmpeg压缩包
	ffmpegDownloadPath := "./bin/ffmpeg.zip"
	err = util.DownloadFile(ffmpegURL, ffmpegDownloadPath, config.Conf.App.Proxy)
	if err != nil {
		log.GetLogger().Error("下载ffmpeg失败", zap.Error(err))
		return err
	}
	// 解压下载的ffmpeg
	err = util.Unzip(ffmpegDownloadPath, "./bin")
	if err != nil {
		log.GetLogger().Error("解压ffmpeg失败", zap.Error(err))
		return err
	}
	log.GetLogger().Info("ffmpeg解压成功")

	// 对于非Windows系统，需要设置可执行权限
	if runtime.GOOS != "windows" {
		err = os.Chmod(ffmpegBinFilePath, 0755)
		if err != nil {
			log.GetLogger().Error("设置文件权限失败", zap.Error(err))
			return err
		}
	}

	// 记录ffmpeg路径供程序后续使用
	storage.FfmpegPath = ffmpegBinFilePath
	log.GetLogger().Info("ffmpeg安装完成", zap.String("路径", ffmpegBinFilePath))

	return nil
}

// checkAndDownloadFfprobe 检测并安装ffprobe
// ffprobe用于获取媒体文件的元数据信息
// 安装逻辑与ffmpeg类似
func checkAndDownloadFfprobe() error {
	// 检查系统环境变量中是否已经有ffprobe
	_, err := exec.LookPath("ffprobe")
	if err == nil {
		log.GetLogger().Info("已找到ffprobe")
		storage.FfprobePath = "ffprobe"
		return nil
	}

	// 构建本地bin目录中ffprobe的路径
	ffprobeBinFilePath := "./bin/ffprobe"
	if runtime.GOOS == "windows" {
		ffprobeBinFilePath += ".exe"
	}
	// 检查之前是否已经下载过ffprobe
	if _, err = os.Stat(ffprobeBinFilePath); err == nil {
		log.GetLogger().Info("已找到ffprobe")
		storage.FfprobePath = ffprobeBinFilePath
		return nil
	}

	log.GetLogger().Info("没有找到ffprobe，即将开始自动安装")
	// 确保./bin目录存在
	err = os.MkdirAll("./bin", 0755)
	if err != nil {
		log.GetLogger().Error("创建./bin目录失败", zap.Error(err))
		return err
	}

	// 根据不同操作系统选择对应的下载链接
	var ffprobeURL string
	if runtime.GOOS == "linux" {
		ffprobeURL = "https://modelscope.cn/models/Maranello/KrillinAI_dependency_cn/resolve/master/ffprobe-6.1-linux-64.zip"
	} else if runtime.GOOS == "darwin" {
		ffprobeURL = "https://modelscope.cn/models/Maranello/KrillinAI_dependency_cn/resolve/master/ffprobe-6.1-macos-64.zip"
	} else if runtime.GOOS == "windows" {
		ffprobeURL = "https://modelscope.cn/models/Maranello/KrillinAI_dependency_cn/resolve/master/ffprobe-6.1-win-64.zip"
	} else {
		log.GetLogger().Error("不支持你当前的操作系统", zap.String("当前系统", runtime.GOOS))
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	// 下载ffprobe压缩包
	ffprobeDownloadPath := "./bin/ffprobe.zip"
	err = util.DownloadFile(ffprobeURL, ffprobeDownloadPath, config.Conf.App.Proxy)
	if err != nil {
		log.GetLogger().Error("下载ffprobe失败", zap.Error(err))
		return err
	}
	// 解压下载的ffprobe
	err = util.Unzip(ffprobeDownloadPath, "./bin")
	if err != nil {
		log.GetLogger().Error("解压ffprobe失败", zap.Error(err))
		return err
	}
	log.GetLogger().Info("ffprobe解压成功")

	// 对于非Windows系统，需要设置可执行权限
	if runtime.GOOS != "windows" {
		err = os.Chmod(ffprobeBinFilePath, 0755)
		if err != nil {
			log.GetLogger().Error("设置文件权限失败", zap.Error(err))
			return err
		}
	}

	// 记录ffprobe路径供程序后续使用
	storage.FfprobePath = ffprobeBinFilePath
	log.GetLogger().Info("ffprobe安装完成", zap.String("路径", ffprobeBinFilePath))

	return nil
}

// checkAndDownloadYtDlp 检测并安装yt-dlp
// yt-dlp是用于从YouTube、Bilibili等视频网站下载视频的工具
func checkAndDownloadYtDlp() error {
	// 检查系统环境变量中是否已经有yt-dlp
	_, err := exec.LookPath("yt-dlp")
	if err == nil {
		log.GetLogger().Info("已找到yt-dlp")
		storage.YtdlpPath = "yt-dlp"
		return nil
	}

	// 构建本地bin目录中yt-dlp的路径
	ytdlpBinFilePath := "./bin/yt-dlp"
	if runtime.GOOS == "windows" {
		ytdlpBinFilePath += ".exe"
	}
	// 检查之前是否已经下载过yt-dlp
	if _, err = os.Stat(ytdlpBinFilePath); err == nil {
		log.GetLogger().Info("已找到ytdlp")
		storage.YtdlpPath = ytdlpBinFilePath
		return nil
	}

	log.GetLogger().Info("没有找到yt-dlp，即将开始自动安装")
	// 确保./bin目录存在
	err = os.MkdirAll("./bin", 0755)
	if err != nil {
		log.GetLogger().Error("创建./bin目录失败", zap.Error(err))
		return err
	}

	// 根据不同操作系统选择对应的下载链接
	var ytDlpURL string
	if runtime.GOOS == "linux" {
		ytDlpURL = "https://modelscope.cn/models/Maranello/KrillinAI_dependency_cn/resolve/master/yt-dlp_linux"
	} else if runtime.GOOS == "darwin" {
		ytDlpURL = "https://modelscope.cn/models/Maranello/KrillinAI_dependency_cn/resolve/master/yt-dlp_macos"
	} else if runtime.GOOS == "windows" {
		ytDlpURL = "https://modelscope.cn/models/Maranello/KrillinAI_dependency_cn/resolve/master/yt-dlp.exe"
	} else {
		log.GetLogger().Error("不支持你当前的操作系统", zap.String("当前系统", runtime.GOOS))
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	// 下载yt-dlp（与前两个工具不同，yt-dlp是直接下载可执行文件，不需要解压）
	err = util.DownloadFile(ytDlpURL, ytdlpBinFilePath, config.Conf.App.Proxy)
	if err != nil {
		log.GetLogger().Error("下载yt-dlp失败", zap.Error(err))
		return err
	}

	// 对于非Windows系统，需要设置可执行权限
	if runtime.GOOS != "windows" {
		err = os.Chmod(ytdlpBinFilePath, 0755)
		if err != nil {
			log.GetLogger().Error("设置文件权限失败", zap.Error(err))
			return err
		}
	}

	// 记录yt-dlp路径供程序后续使用
	storage.YtdlpPath = ytdlpBinFilePath
	log.GetLogger().Info("yt-dlp安装完成", zap.String("路径", ytdlpBinFilePath))

	return nil
}

// checkFasterWhisper 检测faster-whisper环境
// faster-whisper是本地运行的语音识别模型，用于将音频转写为文本
// 注意：目前仅支持Windows和Linux系统
func checkFasterWhisper() error {
	var (
		filePath string
		err      error
	)
	// 根据操作系统确定faster-whisper可执行文件的路径
	if runtime.GOOS == "windows" {
		filePath = "./bin/faster-whisper/Faster-Whisper-XXL/faster-whisper-xxl.exe"
	} else if runtime.GOOS == "linux" {
		filePath = "./bin/faster-whisper/Whisper-Faster-XXL/whisper-faster-xxl"
	} else {
		// 对于不支持的系统（如macOS），返回错误并建议使用其他转写提供商
		return fmt.Errorf("fasterwhisper不支持你当前的操作系统: %s，请选择其它transcription provider", runtime.GOOS)
	}
	// 检查faster-whisper可执行文件是否存在
	if _, err = os.Stat(filePath); os.IsNotExist(err) {
		log.GetLogger().Info("没有找到faster-whisper，即将开始自动下载，文件较大请耐心等待")
		// 确保./bin目录存在
		err = os.MkdirAll("./bin", 0755)
		if err != nil {
			log.GetLogger().Error("创建./bin目录失败", zap.Error(err))
			return err
		}
		// 根据操作系统选择下载链接
		var downloadUrl string
		if runtime.GOOS == "windows" {
			downloadUrl = "https://modelscope.cn/models/Maranello/KrillinAI_dependency_cn/resolve/master/Faster-Whisper-XXL_r194.5_windows.zip"
		} else {
			downloadUrl = "https://modelscope.cn/models/Maranello/KrillinAI_dependency_cn/resolve/master/Faster-Whisper-XXL_r192.3.1_linux.zip"
		}
		// 下载faster-whisper
		err = util.DownloadFile(downloadUrl, "./bin/faster-whisper.zip", config.Conf.App.Proxy)
		if err != nil {
			log.GetLogger().Error("下载faster-whisper失败", zap.Error(err))
			return err
		}
		log.GetLogger().Info("开始解压faster-whisper")
		// 解压下载的faster-whisper
		err = util.Unzip("./bin/faster-whisper.zip", "./bin/faster-whisper/")
		if err != nil {
			log.GetLogger().Error("解压faster-whisper失败", zap.Error(err))
			return err
		}
	}
	// 对于非Windows系统，需要设置可执行权限
	if runtime.GOOS != "windows" {
		err = os.Chmod(filePath, 0755)
		if err != nil {
			log.GetLogger().Error("设置文件权限失败", zap.Error(err))
			return err
		}
	}
	// 记录faster-whisper路径供程序后续使用
	storage.FasterwhisperPath = filePath
	log.GetLogger().Info("faster-whisper检查完成", zap.String("路径", filePath))
	return nil
}

// checkModel 检测并下载本地模型
// 根据不同的whisper类型（fasterwhisper或whisperkit）下载对应的模型文件
// @param whisperType 模型类型："fasterwhisper" 或 "whisperkit"
func checkModel(whisperType string) error {
	var err error
	// 确保whisperkit模型目录存在
	if _, err = os.Stat("./models/whisperkit"); os.IsNotExist(err) {
		err = os.MkdirAll("./models/whisperkit", 0755)
		if err != nil {
			log.GetLogger().Error("创建./models目录失败", zap.Error(err))
			return err
		}
	}
	// 从配置文件获取模型大小设置
	model := config.Conf.LocalModel.Whisper
	var modelPath string // cli中使用的model path
	switch whisperType {
	case "fasterwhisper":
		// fasterwhisper模型路径
		modelPath = fmt.Sprintf("./models/faster-whisper-%s/model.bin", model)
		if _, err = os.Stat(modelPath); os.IsNotExist(err) {
			// 模型文件不存在，开始下载
			log.GetLogger().Info(fmt.Sprintf("没有找到模型文件%s,即将开始自动下载", modelPath))
			downloadUrl := fmt.Sprintf("https://modelscope.cn/models/Maranello/KrillinAI_dependency_cn/resolve/master/faster-whisper-%s.zip", model)
			err = util.DownloadFile(downloadUrl, fmt.Sprintf("./models/faster-whisper-%s.zip", model), config.Conf.App.Proxy)
			if err != nil {
				log.GetLogger().Error("下载fasterwhisper模型失败", zap.Error(err))
				return err
			}
			// 解压模型文件
			err = util.Unzip(fmt.Sprintf("./models/faster-whisper-%s.zip", model), fmt.Sprintf("./models/faster-whisper-%s/", model))
			if err != nil {
				log.GetLogger().Error("解压模型失败", zap.Error(err))
				return err
			}
			log.GetLogger().Info("模型下载完成", zap.String("路径", modelPath))
		}
	case "whisperkit":
		// whisperkit模型路径（macOS专用）
		modelPath = "./models/whisperkit/openai_whisper-large-v2"
		files, _ := os.ReadDir(modelPath)
		if len(files) == 0 {
			// 模型目录为空，开始下载
			log.GetLogger().Info("没有找到whisperkit模型，即将开始自动下载")
			downloadUrl := "https://modelscope.cn/models/Maranello/KrillinAI_dependency_cn/resolve/master/whisperkit-large-v2.zip"
			err = util.DownloadFile(downloadUrl, "./models/whisperkit/openai_whisper-large-v2.zip", config.Conf.App.Proxy)
			if err != nil {
				log.GetLogger().Info("下载whisperkit模型失败", zap.Error(err))
				return err
			}
			// 解压模型文件
			err = util.Unzip("./models/whisperkit/openai_whisper-large-v2.zip", "./models/whisperkit/")
			if err != nil {
				log.GetLogger().Error("解压whisperkit模型失败", zap.Error(err))
				return err
			}
			log.GetLogger().Info("whisperkit模型下载完成", zap.String("路径", modelPath))
		}
	}

	log.GetLogger().Info("模型检查完成", zap.String("路径", modelPath))
	return nil
}

// checkWhisperKit 检测whisperkit环境
// whisperkit是macOS系统上的语音识别工具
// 通过homebrew安装whisperkit-cli
func checkWhisperKit() error {
	// 检查系统中是否已安装whisperkit-cli
	cmd := exec.Command("which", "whisperkit-cli")
	err := cmd.Run()
	if err != nil {
		log.GetLogger().Info("没有找到whisperkit-cli，即将开始自动安装")
		// 通过brew安装whisperkit-cli
		cmd = exec.Command("brew", "install", "whisperkit-cli")
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.GetLogger().Error("whisperkit-cli 安装失败", zap.String("info", string(output)), zap.Error(err))
			return err
		}
		log.GetLogger().Info("whisperkit-cli 安装成功")
	}
	// 记录whisperkit-cli路径供程序后续使用
	storage.WhisperKitPath = "whisperkit-cli"
	log.GetLogger().Info("检测到whisperkit-cli已安装")
	return nil
}
