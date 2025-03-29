package service

import (
	"bufio"
	"bytes"
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
	"runtime"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// embedSubtitles 处理字幕嵌入到视频的主函数
// 根据指定的类型（横屏、竖屏或两者都有）将字幕嵌入到视频中
// ctx: 上下文信息
// stepParam: 字幕任务参数，包含输入视频路径、字幕文件路径等信息
func (s Service) embedSubtitles(ctx context.Context, stepParam *types.SubtitleTaskStepParam) error {
	// 用于记录处理过程中的错误
	var err error
	// 根据指定的嵌入类型进行处理（横屏、竖屏或全部）
	if stepParam.EmbedSubtitleVideoType == "horizontal" || stepParam.EmbedSubtitleVideoType == "vertical" || stepParam.EmbedSubtitleVideoType == "all" {
		// 获取输入视频的分辨率信息
		var width, height int
		width, height, err = getResolution(stepParam.InputVideoPath)
		// 横屏可以合成竖屏的，但竖屏暂时不支持合成横屏的
		if stepParam.EmbedSubtitleVideoType == "horizontal" || stepParam.EmbedSubtitleVideoType == "all" {
			// 检查输入视频是否为横屏（宽>高）
			if width < height {
				log.GetLogger().Info("检测到输入视频是竖屏，无法合成横屏视频，跳过")
				return nil
			}
			log.GetLogger().Info("合成字幕嵌入视频：横屏")
			// 调用embedSubtitles函数处理横屏视频（参数true表示横屏模式）
			err = embedSubtitles(stepParam, true)
			if err != nil {
				log.GetLogger().Error("embedSubtitles embedSubtitles error", zap.Any("step param", stepParam), zap.Error(err))
				return fmt.Errorf("embedSubtitles embedSubtitles error: %w", err)
			}
		}
		if stepParam.EmbedSubtitleVideoType == "vertical" || stepParam.EmbedSubtitleVideoType == "all" {
			if width > height {
				// 如果原视频是横屏，需要先转换为竖屏视频
				// 定义转换后的竖屏视频存储路径
				transferredVerticalVideoPath := filepath.Join(stepParam.TaskBasePath, types.SubtitleTaskTransferredVerticalVideoFileName)
				// 调用convertToVertical函数将横屏视频转换为竖屏格式
				// 该函数会处理视频的布局调整，并添加主标题和副标题
				err = convertToVertical(stepParam.InputVideoPath, transferredVerticalVideoPath, stepParam.VerticalVideoMajorTitle, stepParam.VerticalVideoMinorTitle)
				if err != nil {
					log.GetLogger().Error("embedSubtitles convertToVertical error", zap.Any("step param", stepParam), zap.Error(err))
					return fmt.Errorf("embedSubtitles convertToVertical error: %w", err)
				}
				// 更新输入视频路径为转换后的竖屏视频
				stepParam.InputVideoPath = transferredVerticalVideoPath
			}
			log.GetLogger().Info("合成字幕嵌入视频：竖屏")
			// 调用embedSubtitles函数处理竖屏视频（参数false表示竖屏模式）
			err = embedSubtitles(stepParam, false)
			if err != nil {
				log.GetLogger().Error("embedSubtitles embedSubtitles error", zap.Any("step param", stepParam), zap.Error(err))
				return fmt.Errorf("embedSubtitles embedSubtitles error: %w", err)
			}
		}
		log.GetLogger().Info("字幕嵌入视频成功")
		return nil
	}
	// 如果不是以上三种模式，则不进行字幕嵌入处理
	log.GetLogger().Info("合成字幕嵌入视频：不合成")
	return nil
}

// splitMajorTextInHorizontal 根据语言特性和最大单词数拆分主要文本
//
// 功能说明:
//
//	对于中文、日文等亚洲语言，按字符拆分；对于英文等西方语言，按单词拆分
//	如果文本长度超过指定的最大单词数，会尝试按照2/5和3/5的比例拆分成两行，保证视觉平衡
//
// 参数:
//   - text: 要拆分的原始文本
//   - language: 文本的语言类型，用于决定分割方式
//   - maxWordOneLine: 每行允许的最大单词/字符数
//
// 返回:
//   - 拆分后的文本行数组，如果文本足够短，则只包含原始文本；如果需要拆分，则包含两行文本
//
// 拆分逻辑:
//  1. 对于亚洲语言（中文、日文、韩文等），按单个字符分割
//  2. 对于西方语言（英文等），按空格分割为单词
//  3. 如果总长度小于最大单词数，直接返回原始文本
//  4. 否则，按照2/5处拆分文本，产生两行，并清理标点符号
func splitMajorTextInHorizontal(text string, language types.StandardLanguageName, maxWordOneLine int) []string {
	// 按语言情况分割
	var (
		segments []string
		sep      string
	)
	if language == types.LanguageNameSimplifiedChinese || language == types.LanguageNameTraditionalChinese ||
		language == types.LanguageNameJapanese || language == types.LanguageNameKorean || language == types.LanguageNameThai {
		segments = regexp.MustCompile(`.`).FindAllString(text, -1)
		sep = ""
	} else {
		segments = strings.Split(text, " ")
		sep = " "
	}

	totalWidth := len(segments)

	// 直接返回原句子
	if totalWidth <= maxWordOneLine {
		return []string{text}
	}

	// 确定拆分点，按2/5和3/5的比例拆分
	line1MaxWidth := int(float64(totalWidth) * 2 / 5)
	currentWidth := 0
	splitIndex := 0

	for i, _ := range segments {
		currentWidth++

		// 当达到 2/5 宽度时，设置拆分点
		if currentWidth >= line1MaxWidth {
			splitIndex = i + 1
			break
		}
	}

	// 分割文本，保留原有句子格式

	line1 := util.CleanPunction(strings.Join(segments[:splitIndex], sep))
	line2 := util.CleanPunction(strings.Join(segments[splitIndex:], sep))

	return []string{line1, line2}
}

// splitChineseText 将中文文本按照指定的每行最大字符数进行拆分
// 主要用于处理竖屏模式下的中文字幕
// 返回拆分后的多行文本
func splitChineseText(text string, maxWordLine int) []string {
	var lines []string
	words := []rune(text)
	for i := 0; i < len(words); i += maxWordLine {
		end := i + maxWordLine
		if end > len(words) {
			end = len(words)
		}
		lines = append(lines, string(words[i:end]))
	}
	return lines
}

// parseSrtTime 解析SRT格式的时间字符串（如：00:01:23,456）
// 将其转换为Go的time.Duration类型，便于时间计算
// 返回解析后的时间间隔和可能的错误
func parseSrtTime(timeStr string) (time.Duration, error) {
	timeStr = strings.Replace(timeStr, ",", ".", 1)
	parts := strings.Split(timeStr, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("parseSrtTime invalid time format: %s", timeStr)
	}

	hours, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	minutes, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}
	secondsAndMilliseconds := strings.Split(parts[2], ".")
	if len(secondsAndMilliseconds) != 2 {
		return 0, fmt.Errorf("invalid time format: %s", timeStr)
	}
	seconds, err := strconv.Atoi(secondsAndMilliseconds[0])
	if err != nil {
		return 0, err
	}
	milliseconds, err := strconv.Atoi(secondsAndMilliseconds[1])
	if err != nil {
		return 0, err
	}

	duration := time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds)*time.Second +
		time.Duration(milliseconds)*time.Millisecond

	return duration, nil
}

// formatTimestamp 将时间间隔格式化为ASS字幕格式所需的时间字符串
// 格式如：00:01:23.45
func formatTimestamp(t time.Duration) string {
	hours := int(t.Hours())
	minutes := int(t.Minutes()) % 60
	seconds := int(t.Seconds()) % 60
	milliseconds := int(t.Milliseconds()) % 1000 / 10
	return fmt.Sprintf("%02d:%02d:%02d.%02d", hours, minutes, seconds, milliseconds)
}

// srtToAss 将SRT格式的字幕文件转换为ASS格式
// ASS格式支持更丰富的样式和位置控制，便于嵌入到视频中
// 参数:
//   - inputSRT: 输入的SRT格式字幕文件路径
//   - outputASS: 输出的ASS格式字幕文件路径
//   - isHorizontal: 是否为横屏模式，影响字幕的布局和样式
//   - stepParam: 包含字幕处理的相关参数
//
// 横屏模式下:
//   - 使用专门的横屏ASS模板
//   - 主要文本会根据语言特性进行智能分割
//   - 设置双语字幕，上方为主要语言，下方为次要语言
//
// 竖屏模式下:
//   - 使用专门的竖屏ASS模板
//   - 中文字幕会进行按字符数分割处理，确保每行不超过限定字符数
//   - 英文字幕保持原样显示
//   - 根据字幕内容计算时间比例，确保长字幕有足够的显示时间
func srtToAss(inputSRT, outputASS string, isHorizontal bool, stepParam *types.SubtitleTaskStepParam) error {
	file, err := os.Open(inputSRT)
	if err != nil {
		log.GetLogger().Error("srtToAss Open input srt error", zap.Error(err))
		return fmt.Errorf("srtToAss Open input srt error: %w", err)
	}
	defer file.Close()

	assFile, err := os.Create(outputASS)
	if err != nil {
		log.GetLogger().Error("srtToAss Create output ass error", zap.Error(err))
		return fmt.Errorf("srtToAss Create output ass error: %w", err)
	}
	defer assFile.Close()
	scanner := bufio.NewScanner(file)

	if isHorizontal {
		_, _ = assFile.WriteString(types.AssHeaderHorizontal)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			// 读取时间戳行
			if !scanner.Scan() {
				break
			}
			timestampLine := scanner.Text()
			parts := strings.Split(timestampLine, " --> ")
			if len(parts) != 2 {
				continue // 无效时间戳格式
			}

			startTimeStr := strings.TrimSpace(parts[0])
			endTimeStr := strings.TrimSpace(parts[1])
			startTime, err := parseSrtTime(startTimeStr)
			if err != nil {
				log.GetLogger().Error("srtToAss parseSrtTime error", zap.Error(err))
				return fmt.Errorf("srtToAss parseSrtTime error: %w", err)
			}
			endTime, err := parseSrtTime(endTimeStr)
			if err != nil {
				log.GetLogger().Error("srtToAss parseSrtTime error", zap.Error(err))
				return fmt.Errorf("srtToAss parseSrtTime error: %w", err)
			}

			var subtitleLines []string
			for scanner.Scan() {
				textLine := scanner.Text()
				if textLine == "" {
					break // 字幕块结束
				}
				subtitleLines = append(subtitleLines, textLine)
			}

			if len(subtitleLines) < 2 {
				continue
			}
			var majorTextLanguage types.StandardLanguageName
			if stepParam.SubtitleResultType == types.SubtitleResultTypeBilingualTranslationOnTop { // 一定是bilingual
				majorTextLanguage = stepParam.TargetLanguage
			} else {
				majorTextLanguage = stepParam.OriginLanguage
			}

			majorLine := strings.Join(splitMajorTextInHorizontal(subtitleLines[0], majorTextLanguage, stepParam.MaxWordOneLine), "      \\N")
			minorLine := util.CleanPunction(subtitleLines[1])

			// ASS条目
			startFormatted := formatTimestamp(startTime)
			endFormatted := formatTimestamp(endTime)
			combinedText := fmt.Sprintf("{\\an2}{\\rMajor}%s\\N{\\rMinor}%s", majorLine, minorLine)
			_, _ = assFile.WriteString(fmt.Sprintf("Dialogue: 0,%s,%s,Major,,0,0,0,,%s\n", startFormatted, endFormatted, combinedText))
		}
	} else {
		// TODO 竖屏拆分调优
		_, _ = assFile.WriteString(types.AssHeaderVertical)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			if !scanner.Scan() {
				break
			}
			timestampLine := scanner.Text()
			parts := strings.Split(timestampLine, " --> ")
			if len(parts) != 2 {
				continue // 无效时间戳格式
			}

			startTimeStr := strings.TrimSpace(parts[0])
			endTimeStr := strings.TrimSpace(parts[1])
			startTime, err := parseSrtTime(startTimeStr)
			if err != nil {
				return err
			}
			endTime, err := parseSrtTime(endTimeStr)
			if err != nil {
				return err
			}

			var content string
			scanner.Scan()
			content = scanner.Text()
			if content == "" {
				continue
			}
			totalTime := endTime - startTime

			if !util.ContainsAlphabetic(content) {
				// 处理中文字幕
				chineseLines := splitChineseText(content, 10)
				for i, line := range chineseLines {
					iStart := startTime + time.Duration(float64(i)*float64(totalTime)/float64(len(chineseLines)))
					iEnd := startTime + time.Duration(float64(i+1)*float64(totalTime)/float64(len(chineseLines)))
					if iEnd > endTime {
						iEnd = endTime
					}

					startFormatted := formatTimestamp(iStart)
					endFormatted := formatTimestamp(iEnd)
					cleanedText := util.CleanPunction(line)
					combinedText := fmt.Sprintf("{\\an2}{\\rMajor}%s", cleanedText)
					_, _ = assFile.WriteString(fmt.Sprintf("Dialogue: 0,%s,%s,Major,,0,0,0,,%s\n", startFormatted, endFormatted, combinedText))
				}
			} else {
				// 处理英文字幕
				startFormatted := formatTimestamp(startTime)
				endFormatted := formatTimestamp(endTime)
				cleanedText := util.CleanPunction(content)
				combinedText := fmt.Sprintf("{\\an2}{\\rMinor}%s", cleanedText)
				_, _ = assFile.WriteString(fmt.Sprintf("Dialogue: 0,%s,%s,Minor,,0,0,0,,%s\n", startFormatted, endFormatted, combinedText))
			}
		}
	}
	return nil
}

// embedSubtitles 将字幕嵌入到视频中的核心函数
//
// 功能说明:
//
//	处理SRT字幕文件转换为ASS格式，并使用FFmpeg将字幕嵌入到视频中
//	根据横竖屏模式不同，生成不同的输出文件名和使用不同的字幕样式
//
// 参数:
//   - stepParam: 字幕任务参数，包含输入视频路径、字幕文件路径等信息
//   - isHorizontal: 是否为横屏模式，决定生成文件名和字幕样式
//
// 处理流程:
//  1. 根据是否横屏确定输出文件名（横屏或竖屏）
//  2. 调用srtToAss函数将SRT字幕转换为ASS字幕
//  3. 使用FFmpeg将ASS字幕嵌入视频，保留原始音频
//  4. 输出处理后的视频文件到指定路径
//
// 注意:
//   - 使用'-vf ass'参数让FFmpeg直接支持ASS字幕
//   - 路径中的反斜杠需要替换为正斜杠，以兼容不同操作系统
func embedSubtitles(stepParam *types.SubtitleTaskStepParam, isHorizontal bool) error {
	outputFileName := types.SubtitleTaskVerticalEmbedVideoFileName
	if isHorizontal {
		outputFileName = types.SubtitleTaskHorizontalEmbedVideoFileName
	}
	assPath := filepath.Join(stepParam.TaskBasePath, "formatted_subtitles.ass")

	if err := srtToAss(stepParam.BilingualSrtFilePath, assPath, isHorizontal, stepParam); err != nil {
		log.GetLogger().Error("embedSubtitles srtToAss error", zap.Any("step param", stepParam), zap.Error(err))
		return fmt.Errorf("embedSubtitles srtToAss error: %w", err)
	}

	cmd := exec.Command(storage.FfmpegPath, "-y", "-i", stepParam.InputVideoPath, "-vf", fmt.Sprintf("ass=%s", strings.ReplaceAll(assPath, "\\", "/")), "-c:a", "aac", "-b:a", "192k", filepath.Join(stepParam.TaskBasePath, fmt.Sprintf("/output/%s", outputFileName)))
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.GetLogger().Error("embedSubtitles embed subtitle into video ffmpeg error", zap.String("video path", stepParam.InputVideoPath), zap.String("output", string(output)), zap.Error(err))
		return fmt.Errorf("embedSubtitles embed subtitle into video ffmpeg error: %w", err)
	}
	return nil
}

// getFontPaths 根据不同操作系统获取适合的字体路径
// 返回粗体和常规字体的路径
func getFontPaths() (string, string, error) {
	switch runtime.GOOS {
	case "windows":
		return "C\\:/Windows/Fonts/msyhbd.ttc", "C\\:/Windows/Fonts/msyh.ttc", nil // 在ffmpeg参数里必须这样写
	case "darwin":
		return "/System/Library/Fonts/Supplemental/Arial Bold.ttf", "/System/Library/Fonts/Supplemental/Arial.ttf", nil
	case "linux":
		return "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", nil
	default:
		return "", "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// getResolution 获取视频的分辨率
// 使用FFprobe工具解析视频文件，提取宽度和高度信息
// 返回视频的宽度、高度和可能的错误
func getResolution(inputVideo string) (int, int, error) {
	// 获取视频信息
	cmdArgs := []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=s=x:p=0",
		inputVideo,
	}
	cmd := exec.Command(storage.FfprobePath, cmdArgs...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		log.GetLogger().Error("获取视频分辨率失败", zap.String("output", out.String()), zap.Error(err))
		return 0, 0, err
	}

	output := strings.TrimSpace(out.String())
	dimensions := strings.Split(output, "x")
	if len(dimensions) != 2 {
		log.GetLogger().Error("获取视频分辨率失败", zap.String("output", output))
		return 0, 0, fmt.Errorf("invalid resolution format: %s", output)
	}
	width, _ := strconv.Atoi(dimensions[0])
	height, _ := strconv.Atoi(dimensions[1])
	return width, height, nil
}

// convertToVertical 将横屏视频转换为竖屏格式
//
// 功能说明:
//
//	将横屏视频转换为竖屏格式，适合在移动设备上播放
//	调整视频尺寸，添加黑色边框，并在上方添加主标题和副标题
//
// 参数:
//   - inputVideo: 输入视频路径
//   - outputVideo: 输出视频路径
//   - majorTitle: 主标题文本
//   - minorTitle: 副标题文本
//
// 处理流程:
//  1. 检查输出视频是否已存在，存在则跳过处理
//  2. 根据当前操作系统获取适合的字体路径
//  3. 使用FFmpeg进行以下处理:
//     - 将视频缩放至720x1280，保持原始宽高比
//     - 在视频顶部添加黑色区域用于放置标题
//     - 在顶部绘制主标题（使用粗体字体）和副标题（使用常规字体）
//     - 设置视频比特率、帧率等参数
//  4. 输出处理后的竖屏视频
//
// 视频处理参数说明:
//   - scale=720:1280:force_original_aspect_ratio=decrease: 缩放视频同时保持原始比例
//   - pad=720:1280:(ow-iw)/2:(oh-ih)*2/5: 对视频进行填充，确保视频在竖屏中居中显示
//   - drawbox: 绘制黑色背景区域用于放置标题
//   - drawtext: 绘制标题文本，设置位置、字体大小、颜色等
func convertToVertical(inputVideo, outputVideo, majorTitle, minorTitle string) error {
	if _, err := os.Stat(outputVideo); err == nil {
		log.GetLogger().Info("竖屏视频已存在", zap.String("outputVideo", outputVideo))
		return nil
	}

	fontBold, fontRegular, err := getFontPaths()
	if err != nil {
		log.GetLogger().Error("获取字体路径失败", zap.Error(err))
		return err
	}

	cmdArgs := []string{
		"-i", inputVideo,
		"-vf", fmt.Sprintf("scale=720:1280:force_original_aspect_ratio=decrease,pad=720:1280:(ow-iw)/2:(oh-ih)*2/5,drawbox=y=0:h=100:c=black@1:t=fill,drawtext=text='%s':x=(w-text_w)/2:y=210:fontsize=55:fontcolor=yellow:box=1:boxcolor=black@0.5:fontfile='%s',drawtext=text='%s':x=(w-text_w)/2:y=280:fontsize=40:fontcolor=yellow:box=1:boxcolor=black@0.5:fontfile='%s'",
			majorTitle, fontBold, minorTitle, fontRegular),
		"-r", "30",
		"-b:v", "7587k",
		"-c:a", "aac",
		"-b:a", "192k",
		"-c:v", "libx264",
		"-preset", "fast",
		"-y",
		outputVideo,
	}
	cmd := exec.Command(storage.FfmpegPath, cmdArgs...)
	var output []byte
	output, err = cmd.CombinedOutput()
	if err != nil {
		log.GetLogger().Error("视频转竖屏失败", zap.String("output", string(output)), zap.Error(err))
		return err
	}

	fmt.Printf("竖屏视频已保存到: %s\n", outputVideo)
	return nil
}
