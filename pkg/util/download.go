package util

import (
	"fmt"
	"io"
	"krillin-ai/config"
	"krillin-ai/log"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"
)

// progressWriter 是一个自定义的io.Writer实现
// 用于实时显示文件下载进度、速度和已下载大小
// 通过嵌入到io.TeeReader中实现边下载边显示进度
type progressWriter struct {
	Total      uint64    // 文件总大小（字节）
	Downloaded uint64    // 已下载的大小（字节）
	StartTime  time.Time // 下载开始时间，用于计算下载速度
}

// Write 实现io.Writer接口的方法
// 在每次写入数据时更新下载统计并实时打印进度信息
// @param p 需要写入的字节切片
// @return 写入的字节数和可能的错误
func (pw *progressWriter) Write(p []byte) (int, error) {
	n := len(p)
	pw.Downloaded += uint64(n)

	// 初始化开始时间（仅在第一次写入时设置）
	if pw.StartTime.IsZero() {
		pw.StartTime = time.Now()
	}

	// 计算下载百分比、已用时间和下载速度
	percent := float64(pw.Downloaded) / float64(pw.Total) * 100
	elapsed := time.Since(pw.StartTime).Seconds()
	speed := float64(pw.Downloaded) / 1024 / 1024 / elapsed

	// 实时更新显示下载进度信息（不换行，在同一行刷新）
	fmt.Printf("\r下载进度: %.2f%% (%.2f MB / %.2f MB) | 速度: %.2f MB/s",
		percent,
		float64(pw.Downloaded)/1024/1024,
		float64(pw.Total)/1024/1024,
		speed)

	return n, nil
}

// DownloadFile 下载文件并保存到指定路径，支持代理设置
// 提供实时的下载进度显示，适用于大文件下载
// @param urlStr 要下载的文件URL
// @param filepath 保存文件的本地路径
// @param proxyAddr 代理服务器地址，如为空则直接连接
// @return 可能的错误信息
func DownloadFile(urlStr, filepath, proxyAddr string) error {
	log.GetLogger().Info("开始下载文件", zap.String("url", urlStr))

	// 创建HTTP客户端
	client := &http.Client{}

	// 如果配置了代理，则设置HTTP传输使用代理
	if proxyAddr != "" {
		client.Transport = &http.Transport{
			Proxy: http.ProxyURL(config.Conf.App.ParsedProxy),
		}
	}

	// 发起HTTP GET请求
	resp, err := client.Get(urlStr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 获取文件大小并显示
	size := resp.ContentLength
	fmt.Printf("文件大小: %.2f MB\n", float64(size)/1024/1024)

	// 创建目标文件
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// 创建带有进度显示的Writer
	progress := &progressWriter{
		Total: uint64(size),
	}
	// 创建TeeReader，将下载内容同时写入文件和进度显示器
	reader := io.TeeReader(resp.Body, progress)

	// 执行实际的文件拷贝（下载）操作
	_, err = io.Copy(out, reader)
	if err != nil {
		return err
	}
	fmt.Printf("\n") // 下载完成后换行，避免后续日志显示在同一行

	log.GetLogger().Info("文件下载完成", zap.String("路径", filepath))
	return nil
}
