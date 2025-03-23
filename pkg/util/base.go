package util

import (
	"archive/zip"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

// strWithUpperLowerNum 定义了一个包含所有可能字符的切片
// 包含：
// - 小写字母 (a-z)
// - 大写字母 (A-Z)
// - 数字 (0-9)
// 使用rune类型存储，以支持Unicode字符
var strWithUpperLowerNum = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ123456789")

// GenerateRandStringWithUpperLowerNum 生成指定长度的随机字符串
// 参数：
//   - n: 需要生成的字符串长度
//
// 返回值：
//   - string: 生成的随机字符串
//
// 使用示例：
//   - 生成8位随机字符串：GenerateRandStringWithUpperLowerNum(8)
//   - 生成16位随机字符串：GenerateRandStringWithUpperLowerNum(16)
//
// 实现原理：
// 1. 创建一个长度为n的rune切片
// 2. 对每个位置，从strWithUpperLowerNum中随机选择一个字符
// 3. 将rune切片转换为字符串
func GenerateRandStringWithUpperLowerNum(n int) string {
	// 创建一个长度为n的rune切片，用于存储随机字符
	b := make([]rune, n)

	// 遍历切片的每个位置
	for i := range b {
		// 从strWithUpperLowerNum中随机选择一个字符
		// rand.Intn(len(strWithUpperLowerNum))生成[0, len-1]范围内的随机数
		b[i] = strWithUpperLowerNum[rand.Intn(len(strWithUpperLowerNum))]
	}

	// 将rune切片转换为字符串并返回
	return string(b)
}

func GetYouTubeID(youtubeURL string) (string, error) {
	parsedURL, err := url.Parse(youtubeURL)
	if err != nil {
		return "", err
	}

	if strings.Contains(parsedURL.Path, "watch") {
		queryParams := parsedURL.Query()
		if id, exists := queryParams["v"]; exists {
			return id[0], nil
		}
	} else {
		pathSegments := strings.Split(parsedURL.Path, "/")
		return pathSegments[len(pathSegments)-1], nil
	}

	return "", fmt.Errorf("no video ID found")
}

// GetBilibiliVideoId 从B站视频链接中提取视频ID（BV号）
// 参数：
//   - url: B站视频链接，支持多种格式
//
// 返回值：
//   - string: 视频的BV号，如果无法匹配则返回空字符串
//
// 支持的链接格式示例：
// 1. https://www.bilibili.com/video/BV1GJ411x7h7
// 2. https://bilibili.com/video/BV1GJ411x7h7
// 3. https://www.bilibili.com/video/av170001/BV1GJ411x7h7
func GetBilibiliVideoId(url string) string {
	// 正则表达式解析：
	// ^https:// - 匹配链接开头
	// (?:www\.)? - 可选的www.部分
	// bilibili\.com/ - 匹配bilibili.com域名
	// (?:video/|video/av\d+/) - 匹配video/或video/av数字/路径
	// (BV[a-zA-Z0-9]+) - 捕获组：匹配BV开头的视频ID
	re := regexp.MustCompile(`https://(?:www\.)?bilibili\.com/(?:video/|video/av\d+/)(BV[a-zA-Z0-9]+)`)

	// 查找匹配项
	// FindStringSubmatch返回一个字符串切片，其中：
	// matches[0]是完整匹配的字符串
	// matches[1]是第一个捕获组（BV号）
	matches := re.FindStringSubmatch(url)

	// 如果找到匹配项（matches长度大于1，说明至少有一个捕获组）
	if len(matches) > 1 {
		// 返回匹配到的BV号
		return matches[1]
	}
	return ""
}

// 将浮点数秒数转换为HH:MM:SS,SSS格式的字符串
func FormatTime(seconds float32) string {
	totalSeconds := int(math.Floor(float64(seconds)))             // 获取总秒数
	milliseconds := int((seconds - float32(totalSeconds)) * 1000) // 获取毫秒部分

	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	secs := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d,%03d", hours, minutes, secs, milliseconds)
}

// 判断字符串是否是纯数字（字幕编号）
func IsNumber(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func Unzip(zipFile, destDir string) error {
	zipReader, err := zip.OpenReader(zipFile)
	if err != nil {
		return fmt.Errorf("打开zip文件失败: %v", err)
	}
	defer zipReader.Close()

	err = os.MkdirAll(destDir, 0755)
	if err != nil {
		return fmt.Errorf("创建目标目录失败: %v", err)
	}

	for _, file := range zipReader.File {
		filePath := filepath.Join(destDir, file.Name)

		if file.FileInfo().IsDir() {
			err := os.MkdirAll(filePath, file.Mode())
			if err != nil {
				return fmt.Errorf("创建目录失败: %v", err)
			}
			continue
		}

		destFile, err := os.Create(filePath)
		if err != nil {
			return fmt.Errorf("创建文件失败: %v", err)
		}
		defer destFile.Close()

		zipFileReader, err := file.Open()
		if err != nil {
			return fmt.Errorf("打开zip文件内容失败: %v", err)
		}
		defer zipFileReader.Close()

		_, err = io.Copy(destFile, zipFileReader)
		if err != nil {
			return fmt.Errorf("复制文件内容失败: %v", err)
		}
	}

	return nil
}

func GenerateID() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}

// ChangeFileExtension 修改文件后缀
func ChangeFileExtension(path string, newExt string) string {
	ext := filepath.Ext(path)
	return path[:len(path)-len(ext)] + newExt
}

func CleanPunction(word string) string {
	return strings.TrimFunc(word, func(r rune) bool {
		return unicode.IsPunct(r)
	})
}

func IsAlphabetic(r rune) bool {
	if unicode.IsLetter(r) { // 中文在IsLetter中会返回true
		switch {
		// 英语及其他拉丁字母的范围
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
			return true
		// 扩展拉丁字母（法语、西班牙语等使用的附加字符）
		case r >= '\u00C0' && r <= '\u024F':
			return true
		// 希腊字母
		case r >= '\u0370' && r <= '\u03FF':
			return true
		// 西里尔字母（俄语等）
		case r >= '\u0400' && r <= '\u04FF':
			return true
		default:
			return false
		}
	}
	return false
}

func ContainsAlphabetic(text string) bool {
	for _, r := range text {
		if IsAlphabetic(r) {
			return true
		}
	}
	return false
}

// CopyFile 复制文件
func CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destinationFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destinationFile.Close()

	_, err = io.Copy(destinationFile, sourceFile)
	if err != nil {
		return err
	}

	return destinationFile.Sync()
}
