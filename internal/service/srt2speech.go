package service

// srt2speech.go 实现了将SRT字幕文件转换为语音的核心功能
// 主要功能包括：
// 1. 解析SRT字幕文件
// 2. 使用TTS服务将文本转换为语音
// 3. 处理音频时长，确保与字幕时间轴对齐
// 4. 支持音色克隆功能
// 5. 生成最终的语音文件

import (
	"context"
	"fmt"
	"krillin-ai/internal/storage"
	"krillin-ai/internal/types"
	"krillin-ai/log"
	"krillin-ai/pkg/util"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
)

// srtFileToSpeech 将SRT字幕文件转换为语音文件
// @param ctx - 上下文信息
// @param stepParam - 字幕任务的参数信息，包含任务配置和路径信息
// @return error - 处理过程中的错误信息
func (s Service) srtFileToSpeech(ctx context.Context, stepParam *types.SubtitleTaskStepParam) error {
	if !stepParam.EnableTts {
		return nil
	}
	// Step 1: 解析字幕文件，获取字幕内容和时间信息
	subtitles, err := parseSRT(stepParam.TtsSourceFilePath)
	if err != nil {
		log.GetLogger().Error("srtFileToSpeech parseSRT error", zap.Any("stepParam", stepParam), zap.Error(err))
		return fmt.Errorf("srtFileToSpeech parseSRT error: %w", err)
	}

	var audioFiles []string
	var currentTime time.Time

	// 创建文件记录音频的时间轴信息，用于调试和验证
	durationDetailFile, err := os.Create(filepath.Join(stepParam.TaskBasePath, types.TtsAudioDurationDetailsFileName))
	if err != nil {
		log.GetLogger().Error("srtFileToSpeech create durationDetailFile error", zap.Any("stepParam", stepParam), zap.Error(err))
		return fmt.Errorf("srtFileToSpeech create durationDetailFile error: %w", err)
	}
	defer durationDetailFile.Close()

	// Step 2: 处理音色选择
	// 支持默认音色或自定义克隆音色
	voiceCode := stepParam.TtsVoiceCode
	if stepParam.VoiceCloneAudioUrl != "" {
		var code string
		code, err = s.VoiceCloneClient.CosyVoiceClone("krillinai", stepParam.VoiceCloneAudioUrl)
		if err != nil {
			log.GetLogger().Error("srtFileToSpeech CosyVoiceClone error", zap.Any("stepParam", stepParam), zap.Error(err))
			return fmt.Errorf("srtFileToSpeech CosyVoiceClone error: %w", err)
		}
		voiceCode = code
	}

	for i, sub := range subtitles {
		outputFile := filepath.Join(stepParam.TaskBasePath, fmt.Sprintf("subtitle_%d.wav", i+1))
		err = s.TtsClient.Text2Speech(sub.Text, voiceCode, outputFile)
		if err != nil {
			log.GetLogger().Error("srtFileToSpeech Text2Speech error", zap.Any("stepParam", stepParam), zap.Any("num", i+1), zap.Error(err))
			return fmt.Errorf("srtFileToSpeech Text2Speech error: %w", err)
		}

		// Step 3: 调整音频时长
		startTime, err := time.Parse("15:04:05,000", sub.Start)
		if err != nil {
			log.GetLogger().Error("srtFileToSpeech parse time error", zap.Any("stepParam", stepParam), zap.Any("num", i+1), zap.String("time str", sub.Start), zap.Error(err))
			return fmt.Errorf("srtFileToSpeech parse time error: %w", err)
		}
		endTime, err := time.Parse("15:04:05,000", sub.End)
		if err != nil {
			log.GetLogger().Error("audioToSubtitle.time.Parse error", zap.Any("stepParam", stepParam), zap.Any("num", i+1), zap.String("time str", sub.Start), zap.Error(err))
			return fmt.Errorf("srtFileToSpeech audioToSubtitle.time.Parse error: %w", err)
		}
		if i == 0 {
			// 处理第一条字幕的情况
			// 如果第一条字幕不是从00:00开始，需要在开头增加静音帧
			// 这样做是为了保持生成的语音文件与原视频的时间轴同步
			if startTime.Second() > 0 {
				// 计算需要添加的静音时长（毫秒）
				// 从00:00:00到字幕开始时间的差值
				silenceDurationMs := startTime.Sub(time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)).Milliseconds()
				// 生成静音音频文件路径
				silenceFilePath := filepath.Join(stepParam.TaskBasePath, "silence_0.wav")
				// 调用函数生成指定时长的静音音频文件
				// 将毫秒转换为秒作为参数传入
				err := newGenerateSilence(silenceFilePath, float64(silenceDurationMs)/1000)
				if err != nil {
					log.GetLogger().Error("srtFileToSpeech newGenerateSilence error", zap.Any("stepParam", stepParam), zap.Error(err))
					return fmt.Errorf("srtFileToSpeech newGenerateSilence error: %w", err)
				}
				// 将生成的静音文件添加到待合并的音频文件列表中
				audioFiles = append(audioFiles, silenceFilePath)

				// 计算静音帧的结束时间，用于记录详细的时间信息
				silenceEndTime := currentTime.Add(time.Duration(silenceDurationMs) * time.Millisecond)
				// 将静音帧的时间信息写入详细记录文件
				durationDetailFile.WriteString(fmt.Sprintf("Silence: start=%s, end=%s\n", currentTime.Format("15:04:05,000"), silenceEndTime.Format("15:04:05,000")))
				// 更新当前时间指针为静音结束时间，为后续的音频片段计时做准备
				currentTime = silenceEndTime
			}
		}

		duration := endTime.Sub(startTime).Seconds()
		if i < len(subtitles)-1 {
			// 如果不是最后一条字幕，增加静音帧时长
			nextStartTime, err := time.Parse("15:04:05,000", subtitles[i+1].Start)
			if err != nil {
				log.GetLogger().Error("srtFileToSpeech parse time error", zap.Any("stepParam", stepParam), zap.Any("num", i+2), zap.String("time str", subtitles[i+1].Start), zap.Error(err))
				return fmt.Errorf("srtFileToSpeech parse time error: %w", err)
			}
			if endTime.Before(nextStartTime) {
				duration = nextStartTime.Sub(startTime).Seconds()
			}
		}

		adjustedFile := filepath.Join(stepParam.TaskBasePath, fmt.Sprintf("adjusted_%d.wav", i+1))
		err = adjustAudioDuration(outputFile, adjustedFile, stepParam.TaskBasePath, duration)
		if err != nil {
			log.GetLogger().Error("srtFileToSpeech adjustAudioDuration error", zap.Any("stepParam", stepParam), zap.Any("num", i+1), zap.Error(err))
			return fmt.Errorf("srtFileToSpeech adjustAudioDuration error: %w", err)
		}

		audioFiles = append(audioFiles, adjustedFile)

		// 计算音频的实际时长
		audioDuration, err := util.GetAudioDuration(adjustedFile)
		if err != nil {
			log.GetLogger().Error("srtFileToSpeech GetAudioDuration error", zap.Any("stepParam", stepParam), zap.Any("num", i+1), zap.Error(err))
			return fmt.Errorf("srtFileToSpeech GetAudioDuration error: %w", err)
		}

		// 计算音频的结束时间
		audioEndTime := currentTime.Add(time.Duration(audioDuration*1000) * time.Millisecond)
		// 写入文件
		durationDetailFile.WriteString(fmt.Sprintf("Audio %d: start=%s, end=%s\n", i+1, currentTime.Format("15:04:05,000"), audioEndTime.Format("15:04:05,000")))
		currentTime = audioEndTime
	}

	// Step 6: 拼接所有音频文件
	finalOutput := filepath.Join(stepParam.TaskBasePath, types.TtsResultAudioFileName)
	err = concatenateAudioFiles(audioFiles, finalOutput, stepParam.TaskBasePath)
	if err != nil {
		log.GetLogger().Error("srtFileToSpeech concatenateAudioFiles error", zap.Any("stepParam", stepParam), zap.Error(err))
		return fmt.Errorf("srtFileToSpeech concatenateAudioFiles error: %w", err)
	}
	stepParam.TtsResultFilePath = finalOutput
	// 更新字幕任务信息
	storage.SubtitleTasks[stepParam.TaskId].ProcessPct = 98
	log.GetLogger().Info("srtFileToSpeech success", zap.String("task id", stepParam.TaskId))
	return nil
}

// parseSRT 解析SRT字幕文件
// @param filePath - SRT文件路径
// @return []types.SrtSentenceWithStrTime - 解析后的字幕数组
// @return error - 解析过程中的错误信息
func parseSRT(filePath string) ([]types.SrtSentenceWithStrTime, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("parseSRT read file error: %w", err)
	}

	var subtitles []types.SrtSentenceWithStrTime
	// 使用正则表达式匹配SRT格式：时间码 --> 时间码 + 文本内容
	re := regexp.MustCompile(`(\d{2}:\d{2}:\d{2},\d{3}) --> (\d{2}:\d{2}:\d{2},\d{3})\s+(.+?)\n`)
	matches := re.FindAllStringSubmatch(string(data), -1)

	for _, match := range matches {
		subtitles = append(subtitles, types.SrtSentenceWithStrTime{
			Start: match[1],
			End:   match[2],
			Text:  strings.Replace(match[3], "\n", " ", -1), // 去除换行符，确保文本格式统一
		})
	}

	return subtitles, nil
}

// newGenerateSilence 生成指定时长的静音音频文件
// @param outputAudio - 输出音频文件路径
// @param duration - 静音时长（秒）
// @return error - 生成过程中的错误信息
func newGenerateSilence(outputAudio string, duration float64) error {
	// 使用FFmpeg生成PCM格式的静音文件
	// 参数说明：
	// -f lavfi: 使用lavfi输入格式
	// anullsrc: 生成静音音频源
	// channel_layout=mono: 单声道
	// sample_rate=44100: 采样率44.1kHz
	cmd := exec.Command(storage.FfmpegPath, "-y", "-f", "lavfi", "-i", "anullsrc=channel_layout=mono:sample_rate=44100", "-t",
		fmt.Sprintf("%.3f", duration), "-ar", "44100", "-ac", "1", "-c:a", "pcm_s16le", outputAudio)
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("newGenerateSilence failed to generate PCM silence: %w", err)
	}

	return nil
}

// adjustAudioDuration 调整音频时长，确保与字幕时间轴对齐
// @param inputFile - 输入音频文件路径
// @param outputFile - 输出音频文件路径
// @param taskBasePath - 任务工作目录
// @param subtitleDuration - 目标时长（秒）
// @return error - 处理过程中的错误信息
func adjustAudioDuration(inputFile, outputFile, taskBasePath string, subtitleDuration float64) error {
	// 获取原始音频时长
	audioDuration, err := util.GetAudioDuration(inputFile)
	if err != nil {
		return err
	}

	// 情况1: 音频时长小于字幕时长，需要补充静音
	if audioDuration < subtitleDuration {
		silenceDuration := subtitleDuration - audioDuration
		silenceFile := filepath.Join(taskBasePath, "silence.wav")
		err := newGenerateSilence(silenceFile, silenceDuration)
		if err != nil {
			return fmt.Errorf("error generating silence: %v", err)
		}

		silenceAudioDuration, _ := util.GetAudioDuration(silenceFile)
		log.GetLogger().Info("adjustAudioDuration", zap.Any("silenceDuration", silenceAudioDuration))

		// 拼接音频和静音
		concatFile := filepath.Join(taskBasePath, "concat.txt")
		f, err := os.Create(concatFile)
		if err != nil {
			return fmt.Errorf("adjustAudioDuration create concat file error: %w", err)
		}
		defer os.Remove(concatFile)

		_, err = f.WriteString(fmt.Sprintf("file '%s'\nfile '%s'\n", filepath.Base(inputFile), filepath.Base(silenceFile)))
		if err != nil {
			return fmt.Errorf("adjustAudioDuration write to concat file error: %v", err)
		}
		f.Close()

		cmd := exec.Command(storage.FfmpegPath, "-y", "-f", "concat", "-safe", "0", "-i", concatFile, "-c", "copy", outputFile)
		log.GetLogger().Info("adjustAudioDuration", zap.Any("inputFile", inputFile), zap.Any("outputFile", outputFile), zap.String("run command", cmd.String()))
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			return fmt.Errorf("adjustAudioDuration concat audio and silence  error: %v", err)
		}

		concatFileDuration, _ := util.GetAudioDuration(outputFile)
		log.GetLogger().Info("adjustAudioDuration", zap.Any("concatFileDuration", concatFileDuration))
		return nil
	}

	// 情况2: 音频时长大于字幕时长，需要加速播放
	if audioDuration > subtitleDuration {
		speed := audioDuration / subtitleDuration
		cmd := exec.Command(storage.FfmpegPath, "-y", "-i", inputFile, "-filter:a", fmt.Sprintf("atempo=%.2f", speed), outputFile)
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// 情况3: 音频时长与字幕时长相同，直接复制
	return util.CopyFile(inputFile, outputFile)
}

// concatenateAudioFiles 将多个音频文件拼接成一个完整的音频文件
// @param audioFiles - 待拼接的音频文件路径数组
// @param outputFile - 最终输出的音频文件路径
// @param taskBasePath - 任务工作目录
// @return error - 处理过程中的错误信息
func concatenateAudioFiles(audioFiles []string, outputFile, taskBasePath string) error {
	// 创建临时文件列表
	listFile := filepath.Join(taskBasePath, "audio_list.txt")
	f, err := os.Create(listFile)
	if err != nil {
		return err
	}
	defer os.Remove(listFile)

	// 写入文件列表
	for _, file := range audioFiles {
		_, err := f.WriteString(fmt.Sprintf("file '%s'\n", filepath.Base(file)))
		if err != nil {
			return err
		}
	}
	f.Close()

	// 使用FFmpeg的concat方式合并音频文件
	cmd := exec.Command(storage.FfmpegPath, "-y", "-f", "concat", "-safe", "0", "-i", listFile, "-c", "copy", outputFile)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
