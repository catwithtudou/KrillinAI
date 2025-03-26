// Package service 实现了音频转字幕的核心服务功能
// audio2subtitle.go 主要实现了将音频文件转换为多语言字幕的功能，包括：
// 1. 音频分割：将长音频分割成小段以便处理
// 2. 语音识别：将音频转换为文本
// 3. 文本翻译：将识别出的文本翻译成目标语言
// 4. 字幕生成：生成包含时间戳的字幕文件，支持双语字幕、单语字幕等多种格式
// 5. 字幕优化：支持自动分行、语气词过滤等优化功能

package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"krillin-ai/config"
	"krillin-ai/internal/storage"
	"krillin-ai/internal/types"
	"krillin-ai/log"
	"krillin-ai/pkg/util"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// audioToSubtitle 是音频转字幕的主要处理函数
// 处理流程包括：音频分割、音频转文字、字幕分割等步骤
// @param ctx 上下文信息，用于控制处理流程
// @param stepParam 字幕任务的参数信息
// @return error 处理过程中的错误信息
func (s Service) audioToSubtitle(ctx context.Context, stepParam *types.SubtitleTaskStepParam) error {
	var err error
	err = s.splitAudio(ctx, stepParam)
	if err != nil {
		return fmt.Errorf("audioToSubtitle splitAudio error: %w", err)
	}
	err = s.audioToSrt(ctx, stepParam) // 这里进度更新到90%了
	if err != nil {
		return fmt.Errorf("audioToSubtitle audioToSrt error: %w", err)
	}
	err = s.splitSrt(ctx, stepParam)
	if err != nil {
		return fmt.Errorf("audioToSubtitle splitSrt error: %w", err)
	}
	// 更新字幕任务信息
	storage.SubtitleTasks[stepParam.TaskId].ProcessPct = 95
	return nil
}

// splitAudio 将长音频文件分割成多个小段
// 使用 ffmpeg 进行音频分割，便于后续并行处理
// @param ctx 上下文信息
// @param stepParam 字幕任务的参数信息
// @return error 处理过程中的错误信息
func (s Service) splitAudio(ctx context.Context, stepParam *types.SubtitleTaskStepParam) error {
	log.GetLogger().Info("audioToSubtitle.splitAudio start", zap.String("task id", stepParam.TaskId))
	var err error
	// 使用ffmpeg分割音频
	outputPattern := filepath.Join(stepParam.TaskBasePath, types.SubtitleTaskSplitAudioFileNamePattern) // 输出文件格式
	segmentDuration := config.Conf.App.SegmentDuration * 60                                             // 计算分段时长，转换为秒

	// 构建并执行 ffmpeg 命令进行音频分割
	cmd := exec.Command(
		storage.FfmpegPath,
		"-i", stepParam.AudioFilePath, // 输入文件路径
		"-f", "segment", // 指定输出格式为分段
		"-segment_time", fmt.Sprintf("%d", segmentDuration), // 设置每段时长（秒）
		"-reset_timestamps", "1", // 重置每段的时间戳为0
		"-y",          // 自动覆盖已存在的输出文件
		outputPattern, // 输出文件名模式
	)
	err = cmd.Run()
	if err != nil {
		log.GetLogger().Error("audioToSubtitle splitAudio ffmpeg err", zap.Any("stepParam", stepParam), zap.Error(err))
		return fmt.Errorf("audioToSubtitle splitAudio ffmpeg err: %w", err)
	}

	// 获取分割后的文件列表，使用通配符匹配所有生成的音频文件
	audioFiles, err := filepath.Glob(filepath.Join(stepParam.TaskBasePath, fmt.Sprintf("%s_*.mp3", types.SubtitleTaskSplitAudioFileNamePrefix)))
	if err != nil {
		log.GetLogger().Error("audioToSubtitle splitAudio filepath.Glob err", zap.Any("stepParam", stepParam), zap.Error(err))
		return fmt.Errorf("audioToSubtitle splitAudio filepath.Glob err: %w", err)
	}
	if len(audioFiles) == 0 {
		log.GetLogger().Error("audioToSubtitle splitAudio no audio files found", zap.Any("stepParam", stepParam))
		return errors.New("audioToSubtitle splitAudio no audio files found")
	}

	// 为每个分割后的音频文件创建 SmallAudio 结构体并添加到处理队列中
	num := 1
	for _, audioFile := range audioFiles {
		stepParam.SmallAudios = append(stepParam.SmallAudios, &types.SmallAudio{
			AudioFile: audioFile,
			Num:       num,
		})
		num++
	}

	// 更新字幕任务进度信息
	storage.SubtitleTasks[stepParam.TaskId].ProcessPct = 20

	log.GetLogger().Info("audioToSubtitle.splitAudio end", zap.String("task id", stepParam.TaskId))
	return nil
}

// audioToSrt 将音频转换为字幕文件
// 包括语音识别、文本翻译、生成带时间戳的字幕等步骤
// @param ctx 上下文信息
// @param stepParam 字幕任务的参数信息
// @return error 处理过程中的错误信息
func (s Service) audioToSrt(ctx context.Context, stepParam *types.SubtitleTaskStepParam) error {
	log.GetLogger().Info("audioToSubtitle.audioToSrt start", zap.Any("taskId", stepParam.TaskId))
	var (
		cancel              context.CancelFunc
		stepNum             = 0
		parallelControlChan = make(chan struct{}, config.Conf.App.TranslateParallelNum) // 控制并发数量的通道
		eg                  *errgroup.Group
		stepNumMu           sync.Mutex // 用于保护进度计数器的互斥锁
		err                 error
	)
	// 创建可取消的上下文和错误组，用于管理并行任务
	ctx, cancel = context.WithCancel(ctx)
	defer cancel()
	eg, ctx = errgroup.WithContext(ctx)

	// 对每个音频片段进行并行处理
	for _, audioFileItem := range stepParam.SmallAudios {
		parallelControlChan <- struct{}{} // 信号量模式控制并发数量
		audioFile := audioFileItem
		eg.Go(func() error {
			// 确保资源释放和异常处理
			defer func() {
				<-parallelControlChan // 释放并发控制槽
				if r := recover(); r != nil {
					log.GetLogger().Error("audioToSubtitle.audioToSrt panic recovered", zap.Any("panic", r), zap.String("stack", string(debug.Stack())))
				}
			}()
			// 检查上下文是否已取消
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			// 执行语音识别，最多重试3次
			var transcriptionData *types.TranscriptionData
			for i := 0; i < 3; i++ {
				language := string(stepParam.OriginLanguage)
				if language == "zh_cn" {
					language = "zh" // 中文简体标识转换
				}
				transcriptionData, err = s.Transcriber.Transcription(audioFile.AudioFile, language, stepParam.TaskBasePath)
				if err == nil {
					break
				}
			}
			if err != nil {
				cancel() // 出错时取消所有并行任务
				log.GetLogger().Error("audioToSubtitle audioToSrt Transcription err", zap.Any("stepParam", stepParam), zap.String("audio file", audioFile.AudioFile), zap.Error(err))
				return fmt.Errorf("audioToSubtitle audioToSrt Transcription err: %w", err)
			}

			if transcriptionData.Text == "" {
				log.GetLogger().Info("audioToSubtitle audioToSrt TranscriptionData.Text is empty", zap.Any("stepParam", stepParam), zap.String("audio file", audioFile.AudioFile))
			}

			audioFile.TranscriptionData = transcriptionData

			// 更新任务进度信息（多个步骤中的第一步）
			stepNumMu.Lock()
			stepNum++
			processPct := uint8(20 + 70*stepNum/(len(stepParam.SmallAudios)*2)) // 进度从20%到90%，分两个主要步骤
			stepNumMu.Unlock()
			storage.SubtitleTasks[stepParam.TaskId].ProcessPct = processPct

			// 文本分割和翻译处理
			err = s.splitTextAndTranslate(stepParam.TaskId, stepParam.TaskBasePath, stepParam.TargetLanguage, stepParam.EnableModalFilter, audioFile)
			if err != nil {
				cancel() // 出错时取消所有并行任务
				log.GetLogger().Error("audioToSubtitle audioToSrt splitTextAndTranslate err", zap.Any("stepParam", stepParam), zap.String("audio file", audioFile.AudioFile), zap.Error(err))
				return fmt.Errorf("audioToSubtitle audioToSrt splitTextAndTranslate err: %w", err)
			}

			// 更新任务进度信息（多个步骤中的第二步）
			stepNumMu.Lock()
			stepNum++
			processPct = uint8(20 + 70*stepNum/(len(stepParam.SmallAudios)*2))
			stepNumMu.Unlock()

			storage.SubtitleTasks[stepParam.TaskId].ProcessPct = processPct

			// 生成字幕时间戳
			err = s.generateTimestamps(stepParam.TaskId, stepParam.TaskBasePath, stepParam.OriginLanguage, stepParam.SubtitleResultType, audioFile, stepParam.MaxWordOneLine)
			if err != nil {
				cancel() // 出错时取消所有并行任务
				log.GetLogger().Error("audioToSubtitle audioToSrt generateTimestamps err", zap.Any("stepParam", stepParam), zap.String("audio file", audioFile.AudioFile), zap.Error(err))
				return fmt.Errorf("audioToSubtitle audioToSrt generateTimestamps err: %w", err)
			}
			return nil
		})
	}

	// 等待所有并行任务完成
	if err = eg.Wait(); err != nil {
		log.GetLogger().Error("audioToSubtitle audioToSrt eg.Wait err", zap.Any("taskId", stepParam.TaskId), zap.Error(err))
		return fmt.Errorf("audioToSubtitle audioToSrt eg.Wait err: %w", err)
	}

	// 准备合并各种格式的字幕文件
	originNoTsFiles := make([]string, 0)       // 原始语言无时间戳字幕
	bilingualFiles := make([]string, 0)        // 双语字幕
	shortOriginMixedFiles := make([]string, 0) // 短句原文与译文混合字幕
	shortOriginFiles := make([]string, 0)      // 短句原文字幕
	for i := 1; i <= len(stepParam.SmallAudios); i++ {
		// 收集各类型字幕文件路径
		splitOriginNoTsFile := fmt.Sprintf("%s/%s", stepParam.TaskBasePath, fmt.Sprintf(types.SubtitleTaskSplitSrtNoTimestampFileNamePattern, i))
		originNoTsFiles = append(originNoTsFiles, splitOriginNoTsFile)
		splitBilingualFile := fmt.Sprintf("%s/%s", stepParam.TaskBasePath, fmt.Sprintf(types.SubtitleTaskSplitBilingualSrtFileNamePattern, i))
		bilingualFiles = append(bilingualFiles, splitBilingualFile)
		shortOriginMixedFile := fmt.Sprintf("%s/%s", stepParam.TaskBasePath, fmt.Sprintf(types.SubtitleTaskSplitShortOriginMixedSrtFileNamePattern, i))
		shortOriginMixedFiles = append(shortOriginMixedFiles, shortOriginMixedFile)
		shortOriginFile := fmt.Sprintf("%s/%s", stepParam.TaskBasePath, fmt.Sprintf(types.SubtitleTaskSplitShortOriginSrtFileNamePattern, i))
		shortOriginFiles = append(shortOriginFiles, shortOriginFile)
	}

	// 合并原始无时间戳字幕（主要用于调试和中间过程）
	originNoTsFile := fmt.Sprintf("%s/%s", stepParam.TaskBasePath, types.SubtitleTaskSrtNoTimestampFileName)
	err = util.MergeFile(originNoTsFile, originNoTsFiles...)
	if err != nil {
		log.GetLogger().Error("audioToSubtitle audioToSrt merge originNoTsFile err",
			zap.Any("stepParam", stepParam), zap.Error(err))
		return fmt.Errorf("audioToSubtitle audioToSrt merge originNoTsFile err: %w", err)
	}

	// 合并最终双语字幕（译文+原文或原文+译文）
	bilingualFile := fmt.Sprintf("%s/%s", stepParam.TaskBasePath, types.SubtitleTaskBilingualSrtFileName)
	err = util.MergeSrtFiles(bilingualFile, bilingualFiles...)
	if err != nil {
		log.GetLogger().Error("audioToSubtitle audioToSrt merge BilingualFile err",
			zap.Any("stepParam", stepParam), zap.Error(err))
		return fmt.Errorf("audioToSubtitle audioToSrt merge BilingualFile err: %w", err)
	}

	// 合并最终混合字幕（长译文+短原文格式）
	// 这种格式适合观看时更好地理解原文，译文整句显示，原文分段显示
	shortOriginMixedFile := fmt.Sprintf("%s/%s", stepParam.TaskBasePath, types.SubtitleTaskShortOriginMixedSrtFileName)
	err = util.MergeSrtFiles(shortOriginMixedFile, shortOriginMixedFiles...)
	if err != nil {
		log.GetLogger().Error("audioToSubtitle audioToSrt merge shortOriginMixedFile err",
			zap.Any("stepParam", stepParam), zap.Error(err))
		return fmt.Errorf("audioToSrt merge shortOriginMixedFile err: %w", err)
	}
	stepParam.ShortOriginMixedSrtFilePath = shortOriginMixedFile

	// 合并最终短原文字幕
	// 这种格式适合语言学习，原文被分成短句便于跟读
	shortOriginFile := fmt.Sprintf("%s/%s", stepParam.TaskBasePath, types.SubtitleTaskShortOriginSrtFileName)
	err = util.MergeSrtFiles(shortOriginFile, shortOriginFiles...)
	if err != nil {
		log.GetLogger().Error("audioToSubtitle audioToSrt mergeShortOriginFile err",
			zap.Any("stepParam", stepParam), zap.Error(err))
		return fmt.Errorf("audioToSrt mergeShortOriginFile err: %w", err)
	}

	// 保存双语字幕路径，供后续分割单语字幕使用
	stepParam.BilingualSrtFilePath = bilingualFile

	// 更新字幕任务进度信息到90%
	storage.SubtitleTasks[stepParam.TaskId].ProcessPct = 90

	log.GetLogger().Info("audioToSubtitle.audioToSrt end", zap.Any("taskId", stepParam.TaskId))

	return nil
}

// splitSrt 将双语字幕文件分割成单语字幕文件
// 根据用户设置的字幕类型生成相应的字幕文件
// @param ctx 上下文信息
// @param stepParam 字幕任务的参数信息
// @return error 处理过程中的错误信息
func (s Service) splitSrt(ctx context.Context, stepParam *types.SubtitleTaskStepParam) error {
	log.GetLogger().Info("audioToSubtitle.splitSrt start", zap.Any("task id", stepParam.TaskId))

	// 准备各种字幕文件路径
	originLanguageSrtFilePath := filepath.Join(stepParam.TaskBasePath, types.SubtitleTaskOriginLanguageSrtFileName)
	originLanguageTextFilePath := filepath.Join(stepParam.TaskBasePath, "output", types.SubtitleTaskOriginLanguageTextFileName)
	targetLanguageSrtFilePath := filepath.Join(stepParam.TaskBasePath, types.SubtitleTaskTargetLanguageSrtFileName)
	targetLanguageTextFilePath := filepath.Join(stepParam.TaskBasePath, "output", types.SubtitleTaskTargetLanguageTextFileName)

	// 打开双语字幕文件进行读取
	file, err := os.Open(stepParam.BilingualSrtFilePath)
	if err != nil {
		log.GetLogger().Error("audioToSubtitle splitSrt open bilingual srt file error", zap.Any("stepParam", stepParam), zap.Error(err))
		return fmt.Errorf("audioToSubtitle splitSrt open bilingual srt file error: %w", err)
	}
	defer file.Close()

	// 创建单语字幕和文本文件
	originLanguageSrtFile, err := os.Create(originLanguageSrtFilePath)
	if err != nil {
		log.GetLogger().Error("audioToSubtitle splitSrt create originLanguageSrtFile error", zap.Any("stepParam", stepParam), zap.Error(err))
		return fmt.Errorf("audioToSubtitle splitSrt create originLanguageSrtFile error: %w", err)
	}
	defer originLanguageSrtFile.Close()

	originLanguageTextFile, err := os.Create(originLanguageTextFilePath)
	if err != nil {
		log.GetLogger().Error("audioToSubtitle splitSrt create originLanguageTextFile error", zap.Any("stepParam", stepParam), zap.Error(err))
		return fmt.Errorf("audioToSubtitle splitSrt create originLanguageTextFile error: %w", err)
	}
	defer originLanguageTextFile.Close()

	targetLanguageSrtFile, err := os.Create(targetLanguageSrtFilePath)
	if err != nil {
		log.GetLogger().Error("audioToSubtitle.splitSrt create targetLanguageSrtFile error", zap.Any("stepParam", stepParam), zap.Error(err))
		return fmt.Errorf("audioToSubtitle.splitSrt create targetLanguageSrtFile error: %w", err)
	}
	defer targetLanguageSrtFile.Close()

	targetLanguageTextFile, err := os.Create(targetLanguageTextFilePath)
	if err != nil {
		log.GetLogger().Error("audioToSubtitle.splitSrt create targetLanguageTextFile error", zap.Any("stepParam", stepParam), zap.Error(err))
		return fmt.Errorf("audioToSubtitle.splitSrt create targetLanguageTextFile error: %w", err)
	}
	defer targetLanguageTextFile.Close()

	// 确定字幕翻译位置：是译文在上还是在下
	isTargetOnTop := stepParam.SubtitleResultType == types.SubtitleResultTypeBilingualTranslationOnTop

	// 逐行读取双语字幕文件并处理
	scanner := bufio.NewScanner(file)
	var block []string // 存储一个字幕块的所有行

	for scanner.Scan() {
		line := scanner.Text()
		// 空行标志着一个字幕块的结束
		if line == "" {
			if len(block) > 0 {
				// 处理完整的字幕块，分别保存到原语言和目标语言文件中
				util.ProcessBlock(block, targetLanguageSrtFile, targetLanguageTextFile, originLanguageSrtFile, originLanguageTextFile, isTargetOnTop)
				block = nil
			}
		} else {
			block = append(block, line)
		}
	}
	// 处理文件末尾可能的最后一个字幕块
	if len(block) > 0 {
		util.ProcessBlock(block, targetLanguageSrtFile, targetLanguageTextFile, originLanguageSrtFile, originLanguageTextFile, isTargetOnTop)
	}

	if err = scanner.Err(); err != nil {
		log.GetLogger().Error("audioToSubtitle splitSrt scan bilingual srt file error", zap.Any("stepParam", stepParam), zap.Error(err))
		return fmt.Errorf("audioToSubtitle splitSrt scan bilingual srt file error: %w", err)
	}

	// 添加字幕文件信息到结果中
	// 1. 添加原语言字幕信息
	subtitleInfo := types.SubtitleFileInfo{
		Path:               originLanguageSrtFilePath,
		LanguageIdentifier: string(stepParam.OriginLanguage),
	}
	// 根据用户界面语言设置字幕名称
	if stepParam.UserUILanguage == types.LanguageNameEnglish {
		subtitleInfo.Name = types.GetStandardLanguageName(stepParam.OriginLanguage) + " Subtitle"
	} else if stepParam.UserUILanguage == types.LanguageNameSimplifiedChinese {
		subtitleInfo.Name = types.GetStandardLanguageName(stepParam.OriginLanguage) + " 单语字幕"
	}
	stepParam.SubtitleInfos = append(stepParam.SubtitleInfos, subtitleInfo)

	// 2. 根据字幕结果类型，添加目标语言字幕信息
	if stepParam.SubtitleResultType == types.SubtitleResultTypeTargetOnly || stepParam.SubtitleResultType == types.SubtitleResultTypeBilingualTranslationOnBottom || stepParam.SubtitleResultType == types.SubtitleResultTypeBilingualTranslationOnTop {
		subtitleInfo = types.SubtitleFileInfo{
			Path:               targetLanguageSrtFilePath,
			LanguageIdentifier: string(stepParam.TargetLanguage),
		}
		if stepParam.UserUILanguage == types.LanguageNameEnglish {
			subtitleInfo.Name = types.GetStandardLanguageName(stepParam.TargetLanguage) + " Subtitle"
		} else if stepParam.UserUILanguage == types.LanguageNameSimplifiedChinese {
			subtitleInfo.Name = types.GetStandardLanguageName(stepParam.TargetLanguage) + " 单语字幕"
		}
		stepParam.SubtitleInfos = append(stepParam.SubtitleInfos, subtitleInfo)
	}

	// 3. 如果是双语字幕模式，添加双语字幕信息
	if stepParam.SubtitleResultType == types.SubtitleResultTypeBilingualTranslationOnTop || stepParam.SubtitleResultType == types.SubtitleResultTypeBilingualTranslationOnBottom {
		subtitleInfo = types.SubtitleFileInfo{
			Path:               stepParam.BilingualSrtFilePath,
			LanguageIdentifier: "bilingual",
		}
		if stepParam.UserUILanguage == types.LanguageNameEnglish {
			subtitleInfo.Name = "Bilingual Subtitle"
		} else if stepParam.UserUILanguage == types.LanguageNameSimplifiedChinese {
			subtitleInfo.Name = "双语字幕"
		}
		stepParam.SubtitleInfos = append(stepParam.SubtitleInfos, subtitleInfo)
		// 保存双语字幕路径，供后续生成配音使用
		stepParam.TtsSourceFilePath = stepParam.BilingualSrtFilePath
	}

	log.GetLogger().Info("audioToSubtitle.splitSrt end", zap.Any("task id", stepParam.TaskId))
	return nil
}

// getSentenceTimestamps 获取句子的时间戳信息
// 通过分析语音识别结果中的词时间戳，计算整句话的起止时间
// @param words 语音识别得到的词列表
// @param sentence 需要计算时间戳的句子
// @param lastTs 上一句的结束时间戳
// @param language 语言类型
// @return types.SrtSentence 句子的时间戳信息
// @return []types.Word 句子中的词信息
// @return float64 本句的结束时间戳
// @return error 处理过程中的错误信息
func getSentenceTimestamps(words []types.Word, sentence string, lastTs float64, language types.StandardLanguageName) (types.SrtSentence, []types.Word, float64, error) {
	var srtSt types.SrtSentence
	var sentenceWordList []string
	sentenceWords := make([]types.Word, 0)

	// 根据不同语言类型采用不同的处理策略
	if language == types.LanguageNameEnglish || language == types.LanguageNameGerman || language == types.LanguageNameTurkish || language == types.LanguageNameRussian { // 处理方式不同
		// 针对以单词为基本单位的语言（如英语、德语等）
		sentenceWordList = util.SplitSentence(sentence)
		if len(sentenceWordList) == 0 {
			return srtSt, sentenceWords, 0, fmt.Errorf("getSentenceTimestamps sentence is empty")
		}

		thisLastTs := lastTs
		sentenceWordIndex := 0
		wordNow := words[sentenceWordIndex]

		// 遍历句子中的每个单词，尝试在语音识别结果中匹配对应的时间戳
		for _, sentenceWord := range sentenceWordList {
			for sentenceWordIndex < len(words) {
				// 在语音识别结果中查找匹配当前单词的词
				for sentenceWordIndex < len(words) && !strings.EqualFold(words[sentenceWordIndex].Text, sentenceWord) {
					sentenceWordIndex++
				}

				if sentenceWordIndex >= len(words) {
					break
				}

				wordNow = words[sentenceWordIndex]
				// 确保单词时间戳在上一个时间戳之后
				if wordNow.Start < thisLastTs {
					sentenceWordIndex++
					continue
				} else {
					break
				}
			}

			// 如果没有找到匹配的单词，创建一个没有时间戳的占位单词
			if sentenceWordIndex >= len(words) {
				sentenceWords = append(sentenceWords, types.Word{
					Text: sentenceWord,
				})
				sentenceWordIndex = 0
				continue
			}

			// 找到匹配的单词，添加到结果中
			sentenceWords = append(sentenceWords, wordNow)
			sentenceWordIndex = 0
		}

		// 找到句子中时间戳连续的最大子数组
		beginWordIndex, endWordIndex := findMaxIncreasingSubArray(sentenceWords)
		if (endWordIndex - beginWordIndex) == 0 {
			return srtSt, sentenceWords, 0, errors.New("getSentenceTimestamps no valid sentence")
		}

		// 提取句子的开始和结束时间戳
		beginWord := sentenceWords[beginWordIndex]
		endWord := sentenceWords[endWordIndex-1]

		// 如果找到的所有单词都是连续的，直接使用首尾单词的时间戳
		if endWordIndex-beginWordIndex == len(sentenceWords) {
			srtSt.Start = beginWord.Start
			srtSt.End = endWord.End
			thisLastTs = endWord.End
			return srtSt, sentenceWords, thisLastTs, nil
		}

		// 尝试向前扩展连续时间戳数组
		if beginWordIndex > 0 {
			for i, j := beginWordIndex-1, beginWord.Num-1; i >= 0 && j >= 0; {
				if words[j].Text == "" {
					j--
					continue
				}
				if strings.EqualFold(words[j].Text, sentenceWords[i].Text) {
					beginWord = words[j]
					sentenceWords[i] = beginWord
				} else {
					break
				}

				i--
				j--
			}
		}

		// 尝试向后扩展连续时间戳数组
		if endWordIndex < len(sentenceWords) {
			for i, j := endWordIndex, endWord.Num+1; i < len(sentenceWords) && j < len(words); {
				if words[j].Text == "" {
					j++
					continue
				}
				if strings.EqualFold(words[j].Text, sentenceWords[i].Text) {
					endWord = words[j]
					sentenceWords[i] = endWord
				} else {
					break
				}

				i++
				j++
			}
		}

		// 微调起始单词，如果开始词与第一个词相差不远，使用第一个词的时间戳
		if beginWord.Num > sentenceWords[0].Num && beginWord.Num-sentenceWords[0].Num < 10 {
			beginWord = sentenceWords[0]
		}

		// 微调结束单词，如果结束词与最后一个词相差不远，使用最后一个词的时间戳
		if sentenceWords[len(sentenceWords)-1].Num > endWord.Num && sentenceWords[len(sentenceWords)-1].Num-endWord.Num < 10 {
			endWord = sentenceWords[len(sentenceWords)-1]
		}

		// 设置句子的起止时间戳，确保不早于上一句的结束时间
		srtSt.Start = beginWord.Start
		if srtSt.Start < thisLastTs {
			srtSt.Start = thisLastTs
		}
		srtSt.End = endWord.End
		if beginWord.Num != endWord.Num && endWord.End > thisLastTs {
			thisLastTs = endWord.End
		}

		return srtSt, sentenceWords, thisLastTs, nil
	} else {
		// 针对以字符为基本单位的语言（如中文、日语等）
		sentenceWordList = strings.Split(util.GetRecognizableString(sentence), "")
		if len(sentenceWordList) == 0 {
			return srtSt, sentenceWords, 0, errors.New("getSentenceTimestamps sentence is empty")
		}

		// 这里的sentence words不是字面上连续的，而是可能有重复的字符
		readableSentenceWords := make([]types.Word, 0)
		thisLastTs := lastTs
		sentenceWordIndex := 0
		wordNow := words[sentenceWordIndex]

		// 遍历句子中的每个字符，尝试在语音识别结果中匹配
		for _, sentenceWord := range sentenceWordList {
			for sentenceWordIndex < len(words) {
				// 查找匹配当前字符的词，或以该字符开头的词
				if !strings.EqualFold(words[sentenceWordIndex].Text, sentenceWord) && !strings.HasPrefix(words[sentenceWordIndex].Text, sentenceWord) {
					sentenceWordIndex++
				} else {
					wordNow = words[sentenceWordIndex]
					if wordNow.Start >= thisLastTs {
						// 找到匹配且时间戳合适的词，记录下来
						sentenceWords = append(sentenceWords, wordNow)
					}
					sentenceWordIndex++
				}
			}
			// 重置索引，继续处理下一个字符
			sentenceWordIndex = 0
		}

		// 使用跳跃式查找算法获取句子的时间戳连续部分
		var beginWordIndex, endWordIndex int
		beginWordIndex, endWordIndex, readableSentenceWords = jumpFindMaxIncreasingSubArray(sentenceWords)
		if (endWordIndex - beginWordIndex) == 0 {
			return srtSt, readableSentenceWords, 0, errors.New("getSentenceTimestamps no valid sentence")
		}

		// 提取句子的开始和结束时间戳
		beginWord := sentenceWords[beginWordIndex]
		endWord := sentenceWords[endWordIndex]

		// 设置句子的起止时间戳，确保不早于上一句的结束时间
		srtSt.Start = beginWord.Start
		if srtSt.Start < thisLastTs {
			srtSt.Start = thisLastTs
		}
		srtSt.End = endWord.End
		if beginWord.Num != endWord.Num && endWord.End > thisLastTs {
			thisLastTs = endWord.End
		}

		return srtSt, readableSentenceWords, thisLastTs, nil
	}
}

// findMaxIncreasingSubArray 找到数组中最长的连续递增子数组
// 用于处理词的时间戳序列，确保时间戳的连续性
// @param words 待处理的词数组
// @return int 最长递增子数组的起始索引
// @return int 最长递增子数组的结束索引
func findMaxIncreasingSubArray(words []types.Word) (int, int) {
	if len(words) == 0 {
		return 0, 0
	}

	// 用于记录当前最大递增子数组的起始索引和长度
	maxStart, maxLen := 0, 1
	// 用于记录当前递增子数组的起始索引和长度
	currStart, currLen := 0, 1

	for i := 1; i < len(words); i++ {
		if words[i].Num == words[i-1].Num+1 {
			// 当前元素比前一个元素大，递增序列继续
			currLen++
		} else {
			// 递增序列结束，检查是否是最长的递增序列
			if currLen > maxLen {
				maxStart = currStart
				maxLen = currLen
			}
			// 重新开始新的递增序列
			currStart = i
			currLen = 1
		}
	}

	// 最后需要再检查一次，因为最大递增子数组可能在数组的末尾
	if currLen > maxLen {
		maxStart = currStart
		maxLen = currLen
	}

	// 返回最大递增子数组
	return maxStart, maxStart + maxLen
}

// jumpFindMaxIncreasingSubArray 找到数组中最长的非连续递增子数组
// 主要用于处理中文等字符级别的时间戳匹配
// @param words 待处理的词数组
// @return int 最长递增子数组的起始索引
// @return int 最长递增子数组的结束索引
// @return []types.Word 构建的可读词序列
func jumpFindMaxIncreasingSubArray(words []types.Word) (int, int, []types.Word) {
	if len(words) == 0 {
		return -1, -1, nil
	}

	// dp[i] 表示以 words[i] 结束的递增子数组的长度
	dp := make([]int, len(words))
	// prev[i] 用来记录与当前递增子数组相连的前一个元素的索引
	prev := make([]int, len(words))

	// 初始化，所有的 dp[i] 都是 1，因为每个元素本身就是一个长度为 1 的子数组
	for i := 0; i < len(words); i++ {
		dp[i] = 1
		prev[i] = -1
	}

	maxLen := 0
	startIdx := -1
	endIdx := -1

	// 遍历每一个元素
	for i := 1; i < len(words); i++ {
		// 对比每个元素与之前的元素，检查是否可以构成递增子数组
		for j := 0; j < i; j++ {
			if words[i].Num == words[j].Num+1 {
				if dp[i] < dp[j]+1 {
					dp[i] = dp[j] + 1
					prev[i] = j
				}
			}
		}

		// 更新最大子数组长度和索引
		if dp[i] > maxLen {
			maxLen = dp[i]
			endIdx = i
		}
	}

	// 如果未找到递增子数组，直接返回
	if endIdx == -1 {
		return -1, -1, nil
	}

	// 回溯找到子数组的起始索引
	startIdx = endIdx
	for prev[startIdx] != -1 {
		startIdx = prev[startIdx]
	}

	// 构造结果子数组
	result := []types.Word{}
	for i := startIdx; i != -1; i = prev[i] {
		result = append(result, words[i])
	}

	// 由于是从后往前构造的子数组，需要反转
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return startIdx, endIdx, result
}

// generateTimestamps 为字幕生成时间戳
// 处理字幕的时间戳分配，支持多种字幕格式的生成
// @param taskId 任务ID
// @param basePath 任务基础路径
// @param originLanguage 原始语言
// @param resultType 字幕结果类型
// @param audioFile 音频文件信息
// @param originLanguageWordOneLine 原语言每行最大词数
// @return error 处理过程中的错误信息
func (s Service) generateTimestamps(taskId, basePath string, originLanguage types.StandardLanguageName,
	resultType types.SubtitleResultType, audioFile *types.SmallAudio, originLanguageWordOneLine int) error {
	// 检查字幕文件是否有文本内容
	srtNoTsFile, err := os.Open(audioFile.SrtNoTsFile)
	if err != nil {
		log.GetLogger().Error("audioToSubtitle generateTimestamps open SrtNoTsFile error", zap.String("taskId", taskId), zap.Error(err))
		return fmt.Errorf("audioToSubtitle generateTimestamps open SrtNoTsFile error: %w", err)
	}
	scanner := bufio.NewScanner(srtNoTsFile)
	if scanner.Scan() {
		if strings.Contains(scanner.Text(), "[无文本]") {
			return nil // 无文本内容，直接返回
		}
	}
	srtNoTsFile.Close()

	// 读取无时间戳的字幕内容
	srtBlocks, err := util.ParseSrtNoTsToSrtBlock(audioFile.SrtNoTsFile)
	if err != nil {
		log.GetLogger().Error("audioToSubtitle generateTimestamps read SrtBlocks error", zap.String("taskId", taskId), zap.Error(err))
		return fmt.Errorf("audioToSubtitle generateTimestamps read SrtBlocks error: %w", err)
	}
	if len(srtBlocks) == 0 {
		return nil
	}

	// 为每个字幕块生成时间戳
	var lastTs float64 // 记录上一句的结束时间戳
	// 存储短句原文字幕的映射，key是原始字幕索引，value是一组短句字幕块
	shortOriginSrtMap := make(map[int][]util.SrtBlock, 0)

	for _, srtBlock := range srtBlocks {
		if srtBlock.OriginLanguageSentence == "" {
			continue
		}
		// 获取句子的时间戳信息
		sentenceTs, sentenceWords, ts, err := getSentenceTimestamps(audioFile.TranscriptionData.Words, srtBlock.OriginLanguageSentence, lastTs, originLanguage)
		if err != nil || ts < lastTs {
			continue
		}

		// 计算实际时间戳，考虑分段偏移
		tsOffset := float64(config.Conf.App.SegmentDuration) * 60 * float64(audioFile.Num-1)
		srtBlock.Timestamp = fmt.Sprintf("%s --> %s", util.FormatTime(float32(sentenceTs.Start+tsOffset)), util.FormatTime(float32(sentenceTs.End+tsOffset)))

		// 处理短句原文字幕的生成
		var (
			originSentence string     // 当前处理的原文短句
			startWord      types.Word // 短句开始单词
			endWord        types.Word // 短句结束单词
		)

		// 如果句子单词数不超过每行限制，直接作为一个短句处理
		if len(sentenceWords) <= originLanguageWordOneLine {
			shortOriginSrtMap[srtBlock.Index] = append(shortOriginSrtMap[srtBlock.Index], util.SrtBlock{
				Index:                  srtBlock.Index,
				Timestamp:              fmt.Sprintf("%s --> %s", util.FormatTime(float32(sentenceTs.Start+tsOffset)), util.FormatTime(float32(sentenceTs.End+tsOffset))),
				OriginLanguageSentence: srtBlock.OriginLanguageSentence,
			})
			lastTs = ts
			continue
		}

		// 动态计算每行单词数，根据句子长度自适应调整
		thisLineWord := originLanguageWordOneLine
		if len(sentenceWords) > originLanguageWordOneLine && len(sentenceWords) <= 2*originLanguageWordOneLine {
			thisLineWord = len(sentenceWords)/2 + 1
		} else if len(sentenceWords) > 2*originLanguageWordOneLine && len(sentenceWords) <= 3*originLanguageWordOneLine {
			thisLineWord = len(sentenceWords)/3 + 1
		} else if len(sentenceWords) > 3*originLanguageWordOneLine && len(sentenceWords) <= 4*originLanguageWordOneLine {
			thisLineWord = len(sentenceWords)/4 + 1
		} else if len(sentenceWords) > 4*originLanguageWordOneLine && len(sentenceWords) <= 5*originLanguageWordOneLine {
			thisLineWord = len(sentenceWords)/5 + 1
		}

		// 根据计算的每行单词数，将长句分割成多个短句
		i := 1
		nextStart := true // 标记是否需要开始一个新的短句

		for _, word := range sentenceWords {
			if nextStart {
				// 开始一个新短句，设置起始单词
				startWord = word
				if startWord.Start < lastTs {
					startWord.Start = lastTs
				}
				if startWord.Start < endWord.End {
					startWord.Start = endWord.End
				}

				if startWord.Start < sentenceTs.Start {
					startWord.Start = sentenceTs.Start
				}
				// 检查时间戳有效性
				if startWord.End > sentenceTs.End {
					originSentence += word.Text + " "
					continue
				}
				originSentence += word.Text + " "
				endWord = startWord
				i++
				nextStart = false
				continue
			}

			// 继续当前短句，累加单词文本
			originSentence += word.Text + " "
			if endWord.End < word.End {
				endWord = word
			}

			if endWord.End > sentenceTs.End {
				endWord.End = sentenceTs.End
			}

			// 达到当前行的单词数限制，创建一个短句字幕块
			if i%thisLineWord == 0 && i > 1 {
				shortOriginSrtMap[srtBlock.Index] = append(shortOriginSrtMap[srtBlock.Index], util.SrtBlock{
					Index:                  srtBlock.Index,
					Timestamp:              fmt.Sprintf("%s --> %s", util.FormatTime(float32(startWord.Start+tsOffset)), util.FormatTime(float32(endWord.End+tsOffset))),
					OriginLanguageSentence: originSentence,
				})
				originSentence = ""
				nextStart = true
			}
			i++
		}

		// 处理剩余的单词，如果有的话
		if originSentence != "" {
			shortOriginSrtMap[srtBlock.Index] = append(shortOriginSrtMap[srtBlock.Index], util.SrtBlock{
				Index:                  srtBlock.Index,
				Timestamp:              fmt.Sprintf("%s --> %s", util.FormatTime(float32(startWord.Start+tsOffset)), util.FormatTime(float32(endWord.End+tsOffset))),
				OriginLanguageSentence: originSentence,
			})
		}
		lastTs = ts
	}

	// 创建并写入双语字幕文件
	finalBilingualSrtFileName := fmt.Sprintf("%s/%s", basePath, fmt.Sprintf(types.SubtitleTaskSplitBilingualSrtFileNamePattern, audioFile.Num))
	finalBilingualSrtFile, err := os.Create(finalBilingualSrtFileName)
	if err != nil {
		log.GetLogger().Error("audioToSubtitle generateTimestamps create bilingual srt file error", zap.String("taskId", taskId), zap.Error(err))
		return fmt.Errorf("audioToSubtitle generateTimestamps create bilingual srt file error: %w", err)
	}
	defer finalBilingualSrtFile.Close()

	// 根据字幕样式写入双语字幕内容
	for _, srtBlock := range srtBlocks {
		_, _ = finalBilingualSrtFile.WriteString(fmt.Sprintf("%d\n", srtBlock.Index))
		_, _ = finalBilingualSrtFile.WriteString(srtBlock.Timestamp + "\n")
		if resultType == types.SubtitleResultTypeBilingualTranslationOnTop {
			// 译文在上方样式
			_, _ = finalBilingualSrtFile.WriteString(srtBlock.TargetLanguageSentence + "\n")
			_, _ = finalBilingualSrtFile.WriteString(srtBlock.OriginLanguageSentence + "\n\n")
		} else {
			// 原文在上方样式（包括on bottom和单语类型）
			_, _ = finalBilingualSrtFile.WriteString(srtBlock.OriginLanguageSentence + "\n")
			_, _ = finalBilingualSrtFile.WriteString(srtBlock.TargetLanguageSentence + "\n\n")
		}
	}

	// 创建并写入混合字幕文件（长译文+短原文格式）
	srtShortOriginMixedFileName := fmt.Sprintf("%s/%s", basePath, fmt.Sprintf(types.SubtitleTaskSplitShortOriginMixedSrtFileNamePattern, audioFile.Num))
	srtShortOriginMixedFile, err := os.Create(srtShortOriginMixedFileName)
	if err != nil {
		log.GetLogger().Error("audioToSubtitle generateTimestamps create srtShortOriginMixedFile err", zap.String("taskId", taskId), zap.Error(err))
		return fmt.Errorf("audioToSubtitle generateTimestamps create srtShortOriginMixedFile err: %w", err)
	}
	defer srtShortOriginMixedFile.Close()

	// 创建并写入短原文字幕文件
	srtShortOriginFileName := fmt.Sprintf("%s/%s", basePath, fmt.Sprintf(types.SubtitleTaskSplitShortOriginSrtFileNamePattern, audioFile.Num))
	srtShortOriginFile, err := os.Create(srtShortOriginFileName)
	if err != nil {
		log.GetLogger().Error("audioToSubtitle generateTimestamps create srtShortOriginFile err", zap.String("taskId", taskId), zap.Error(err))
		return fmt.Errorf("audioToSubtitle generateTimestamps create srtShortOriginFile err: %w", err)
	}
	defer srtShortOriginMixedFile.Close()

	// 初始化字幕序号计数器
	mixedSrtNum := 1
	shortSrtNum := 1

	// 写入混合和短原文字幕内容
	for _, srtBlock := range srtBlocks {
		// 先写入译文部分（整句）
		srtShortOriginMixedFile.WriteString(fmt.Sprintf("%d\n", mixedSrtNum))
		srtShortOriginMixedFile.WriteString(srtBlock.Timestamp + "\n")
		srtShortOriginMixedFile.WriteString(srtBlock.TargetLanguageSentence + "\n\n")
		mixedSrtNum++

		// 再写入原文短句部分
		shortOriginSentence := shortOriginSrtMap[srtBlock.Index]
		for _, shortOriginBlock := range shortOriginSentence {
			// 写入混合字幕文件
			srtShortOriginMixedFile.WriteString(fmt.Sprintf("%d\n", mixedSrtNum))
			srtShortOriginMixedFile.WriteString(shortOriginBlock.Timestamp + "\n")
			srtShortOriginMixedFile.WriteString(shortOriginBlock.OriginLanguageSentence + "\n\n")
			mixedSrtNum++

			// 写入短原文字幕文件
			srtShortOriginFile.WriteString(fmt.Sprintf("%d\n", shortSrtNum))
			srtShortOriginFile.WriteString(shortOriginBlock.Timestamp + "\n")
			srtShortOriginFile.WriteString(shortOriginBlock.OriginLanguageSentence + "\n\n")
			shortSrtNum++
		}
	}

	return nil
}

// splitTextAndTranslate 分割文本并进行翻译
// 将识别出的文本分割成合适的语句，并翻译成目标语言
// @param taskId 任务ID
// @param baseTaskPath 任务基础路径
// @param targetLanguage 目标语言
// @param enableModalFilter 是否启用语气词过滤
// @param audioFile 音频文件信息
// @return error 处理过程中的错误信息
func (s Service) splitTextAndTranslate(taskId, baseTaskPath string, targetLanguage types.StandardLanguageName, enableModalFilter bool, audioFile *types.SmallAudio) error {
	var (
		splitContent string // 分割后的内容
		splitPrompt  string // 提示模板
		err          error
	)
	// 选择合适的提示模板，根据是否启用语气词过滤
	if enableModalFilter {
		splitPrompt = fmt.Sprintf(types.SplitTextPromptWithModalFilter, types.GetStandardLanguageName(targetLanguage))
	} else {
		splitPrompt = fmt.Sprintf(types.SplitTextPrompt, types.GetStandardLanguageName(targetLanguage))
	}

	// 检查源文本是否为空
	if audioFile.TranscriptionData.Text == "" {
		return fmt.Errorf("audioToSubtitle splitTextAndTranslate audioFile.TranscriptionData.Text is empty")
	}

	// 最多尝试4次获取有效的翻译结果
	for i := 0; i < 4; i++ {
		// 调用AI接口进行文本分割和翻译
		splitContent, err = s.ChatCompleter.ChatCompletion(splitPrompt + audioFile.TranscriptionData.Text)
		if err != nil {
			log.GetLogger().Warn("audioToSubtitle splitTextAndTranslate ChatCompletion error, retrying...",
				zap.Any("taskId", taskId), zap.Int("attempt", i+1), zap.Error(err))
			continue
		}

		// 验证返回内容的格式和原文匹配度
		if isValidSplitContent(splitContent, audioFile.TranscriptionData.Text) {
			break // 验证通过，结束重试
		}

		log.GetLogger().Warn("audioToSubtitle splitTextAndTranslate invalid response format or content mismatch, retrying...",
			zap.Any("taskId", taskId), zap.Int("attempt", i+1))
		err = fmt.Errorf("invalid split content format or content mismatch")
	}

	// 处理所有重试后仍失败的情况
	if err != nil {
		log.GetLogger().Error("audioToSubtitle splitTextAndTranslate failed after retries", zap.Any("taskId", taskId), zap.Error(err))
		return fmt.Errorf("audioToSubtitle splitTextAndTranslate error: %w", err)
	}

	// 保存分割和翻译后的字幕内容到文件
	originNoTsSrtFile := fmt.Sprintf("%s/%s", baseTaskPath, fmt.Sprintf(types.SubtitleTaskSplitSrtNoTimestampFileNamePattern, audioFile.Num))
	err = os.WriteFile(originNoTsSrtFile, []byte(splitContent), 0644)
	if err != nil {
		log.GetLogger().Error("audioToSubtitle splitTextAndTranslate write originNoTsSrtFile err", zap.Any("taskId", taskId), zap.Error(err))
		return fmt.Errorf("audioToSubtitle splitTextAndTranslate write originNoTsSrtFile err: %w", err)
	}

	// 记录字幕文件路径，供后续处理使用
	audioFile.SrtNoTsFile = originNoTsSrtFile
	return nil
}

// isValidSplitContent 验证分割后的内容是否符合格式要求，并检查原文字数是否与输入文本相近
func isValidSplitContent(splitContent, originalText string) bool {
	// 处理空内容情况
	if splitContent == "" || originalText == "" {
		return splitContent == "" && originalText == "" // 两者都为空才算有效
	}

	// 处理特殊标记：无文本情况
	if strings.Contains(splitContent, "[无文本]") {
		return originalText == "" || len(strings.TrimSpace(originalText)) < 10 // 原文为空或很短时有效
	}

	// 分割内容按行解析
	lines := strings.Split(splitContent, "\n")
	if len(lines) < 3 { // 至少需要一个完整的字幕块（序号+译文+原文）
		return false
	}

	var originalLines []string // 存储提取的原文行
	var isValidFormat bool     // 标记是否找到有效格式

	// 逐行解析内容，验证格式并提取原文
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		// 检查是否为序号行（字幕块的开始）
		if _, err := strconv.Atoi(line); err == nil {
			if i+2 >= len(lines) {
				log.GetLogger().Warn("audioToSubtitle invaild Format", zap.Any("splitContent", splitContent), zap.Any("line", line))
				return false
			}
			// 收集原文行（序号之后的第三行），并去除可能的方括号
			originalLine := strings.TrimSpace(lines[i+2])
			originalLine = strings.TrimPrefix(originalLine, "[")
			originalLine = strings.TrimSuffix(originalLine, "]")
			originalLines = append(originalLines, originalLine)
			i += 2 // 跳过翻译行和原文行
			isValidFormat = true
		}
	}

	// 格式检查：必须找到至少一个有效的字幕块
	if !isValidFormat || len(originalLines) == 0 {
		log.GetLogger().Warn("audioToSubtitle invaild Format", zap.Any("splitContent", splitContent))
		return false
	}

	// 内容完整性检查：合并提取的原文并与原始文本比较字数
	combinedOriginal := strings.Join(originalLines, "")
	originalTextLength := len(strings.TrimSpace(originalText))
	combinedLength := len(strings.TrimSpace(combinedOriginal))

	// 允许200字符的差异，考虑翻译和分割过程中的一些字符变化
	return math.Abs(float64(originalTextLength-combinedLength)) <= 200
}
