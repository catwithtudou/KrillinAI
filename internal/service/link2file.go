package service

import (
	"context"
	"errors"
	"fmt"
	"krillin-ai/config"
	"krillin-ai/internal/storage"
	"krillin-ai/internal/types"
	"krillin-ai/log"
	"krillin-ai/pkg/util"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

// linkToFile 处理视频链接并提取音频文件
// 支持三种类型的输入：
// 1. 本地文件 (local:)
// 2. YouTube视频
// 3. Bilibili视频
//
// 参数：
//   - ctx: 上下文信息
//   - stepParam: 字幕任务步骤参数
//
// 返回值：
//   - error: 处理过程中的错误信息
func (s Service) linkToFile(ctx context.Context, stepParam *types.SubtitleTaskStepParam) error {
	var (
		err    error
		output []byte
	)
	// 初始化文件路径
	link := stepParam.Link
	audioPath := fmt.Sprintf("%s/%s", stepParam.TaskBasePath, types.SubtitleTaskAudioFileName)
	videoPath := fmt.Sprintf("%s/%s", stepParam.TaskBasePath, types.SubtitleTaskVideoFileName)
	// 更新任务进度为3%
	storage.SubtitleTasks[stepParam.TaskId].ProcessPct = 3

	// 1. 处理本地文件
	if strings.Contains(link, "local:") {
		// 移除local:前缀，获取实际文件路径
		videoPath = strings.ReplaceAll(link, "local:", "")
		stepParam.InputVideoPath = videoPath
		// 使用ffmpeg提取音频
		// 参数说明：
		// -i: 输入文件
		// -vn: 不处理视频
		// -ar 44100: 采样率44.1kHz
		// -ac 2: 双声道
		// -ab 192k: 音频比特率192kbps
		// -f mp3: 输出MP3格式
		cmd := exec.Command(storage.FfmpegPath, "-i", videoPath, "-vn", "-ar", "44100", "-ac", "2", "-ab", "192k", "-f", "mp3", audioPath)
		output, err = cmd.CombinedOutput()
		if err != nil {
			log.GetLogger().Error("generateAudioSubtitles.linkToFile ffmpeg error", zap.Any("step param", stepParam), zap.String("output", string(output)), zap.Error(err))
			return fmt.Errorf("generateAudioSubtitles.linkToFile ffmpeg error: %w", err)
		}
	} else if strings.Contains(link, "youtube.com") { // 2. 处理YouTube视频
		// 提取YouTube视频ID
		var videoId string
		videoId, err = util.GetYouTubeID(link)
		if err != nil {
			log.GetLogger().Error("linkToFile.GetYouTubeID error", zap.Any("step param", stepParam), zap.Error(err))
			return fmt.Errorf("linkToFile.GetYouTubeID error: %w", err)
		}
		// 构造标准YouTube链接
		stepParam.Link = "https://www.youtube.com/watch?v=" + videoId
		// 使用yt-dlp下载音频
		// 参数说明：
		// -f bestaudio: 选择最佳音频质量
		// --extract-audio: 提取音频
		// --audio-format mp3: 转换为MP3格式
		// --audio-quality 192K: 设置音频质量
		cmdArgs := []string{"-f", "bestaudio", "--extract-audio", "--audio-format", "mp3", "--audio-quality", "192K", "-o", audioPath, stepParam.Link}
		// 添加代理设置（如果配置了）
		if config.Conf.App.Proxy != "" {
			cmdArgs = append(cmdArgs, "--proxy", config.Conf.App.Proxy)
		}
		// 添加cookies文件（用于访问受限内容）
		cmdArgs = append(cmdArgs, "--cookies", "./cookies.txt")
		// 指定ffmpeg路径（如果不是系统默认路径）
		if storage.FfmpegPath != "ffmpeg" {
			cmdArgs = append(cmdArgs, "--ffmpeg-location", storage.FfmpegPath)
		}
		cmd := exec.Command(storage.YtdlpPath, cmdArgs...)
		output, err = cmd.CombinedOutput()
		if err != nil {
			log.GetLogger().Error("linkToFile download audio yt-dlp error", zap.Any("step param", stepParam), zap.String("output", string(output)), zap.Error(err))
			return fmt.Errorf("linkToFile download audio yt-dlp error: %w", err)
		}
	} else if strings.Contains(link, "bilibili.com") { // 3. 处理Bilibili视频
		// 提取Bilibili视频ID
		videoId := util.GetBilibiliVideoId(link)
		if videoId == "" {
			return errors.New("linkToFile error: invalid link")
		}
		// 构造标准Bilibili链接
		stepParam.Link = "https://www.bilibili.com/video/" + videoId
		// 使用yt-dlp下载音频
		// 参数说明：
		// -f bestaudio[ext=m4a]: 选择最佳m4a格式音频
		// -x: 提取音频
		// --audio-format mp3: 转换为MP3格式
		cmdArgs := []string{"-f", "bestaudio[ext=m4a]", "-x", "--audio-format", "mp3", "-o", audioPath, stepParam.Link}
		// 添加代理设置（如果配置了）
		if config.Conf.App.Proxy != "" {
			cmdArgs = append(cmdArgs, "--proxy", config.Conf.App.Proxy)
		}
		// 指定ffmpeg路径（如果不是系统默认路径）
		if storage.FfmpegPath != "ffmpeg" {
			cmdArgs = append(cmdArgs, "--ffmpeg-location", storage.FfmpegPath)
		}
		cmd := exec.Command(storage.YtdlpPath, cmdArgs...)
		output, err = cmd.CombinedOutput()
		if err != nil {
			log.GetLogger().Error("linkToFile download audio yt-dlp error", zap.Any("step param", stepParam), zap.String("output", string(output)), zap.Error(err))
			return fmt.Errorf("linkToFile download audio yt-dlp error: %w", err)
		}
	} else {
		// 不支持的视频源
		log.GetLogger().Info("linkToFile.unsupported link type", zap.Any("step param", stepParam))
		return errors.New("linkToFile error: unsupported link, only support youtube, bilibili and local file")
	}

	// 更新任务进度为6%
	storage.SubtitleTasks[stepParam.TaskId].ProcessPct = 6
	// 保存音频文件路径
	stepParam.AudioFilePath = audioPath

	// 如果需要下载原视频（非本地文件且需要嵌入字幕）
	if !strings.HasPrefix(link, "local:") && stepParam.EmbedSubtitleVideoType != "none" {
		// 使用yt-dlp下载视频
		// 参数说明：
		// -f bestvideo[height<=1080][ext=mp4]+bestaudio[ext=m4a]/...: 选择最佳视频质量（按分辨率优先级）
		cmdArgs := []string{"-f", "bestvideo[height<=1080][ext=mp4]+bestaudio[ext=m4a]/bestvideo[height<=720][ext=mp4]+bestaudio[ext=m4a]/bestvideo[height<=480][ext=mp4]+bestaudio[ext=m4a]", "-o", videoPath, stepParam.Link}
		// 添加代理设置（如果配置了）
		if config.Conf.App.Proxy != "" {
			cmdArgs = append(cmdArgs, "--proxy", config.Conf.App.Proxy)
		}
		cmd := exec.Command(storage.YtdlpPath, cmdArgs...)
		output, err = cmd.CombinedOutput()
		if err != nil {
			log.GetLogger().Error("linkToFile download video yt-dlp error", zap.Any("step param", stepParam), zap.String("output", string(output)), zap.Error(err))
			return fmt.Errorf("linkToFile download video yt-dlp error: %w", err)
		}
		// 保存视频文件路径
		stepParam.InputVideoPath = videoPath
	}

	// 更新任务进度为10%
	storage.SubtitleTasks[stepParam.TaskId].ProcessPct = 10
	return nil
}
