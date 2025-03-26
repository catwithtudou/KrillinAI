// Package aliyun 提供阿里云相关服务的客户端实现
package aliyun

import (
	"context"
	"fmt"
	"os"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
)

// OssClient 封装了阿里云 OSS 客户端的基本操作
// 内嵌了 oss.Client 以复用其方法
type OssClient struct {
	*oss.Client
	Bucket string // 默认的存储桶名称
}

// NewOssClient 创建一个新的 OSS 客户端实例
// 参数:
//   - accessKeyID: 阿里云访问密钥 ID
//   - accessKeySecret: 阿里云访问密钥密码
//   - bucket: 默认的存储桶名称
//
// 返回:
//   - *OssClient: OSS 客户端实例
func NewOssClient(accessKeyID, accessKeySecret, bucket string) *OssClient {
	// 创建静态凭证提供器
	credProvider := credentials.NewStaticCredentialsProvider(accessKeyID, accessKeySecret)

	// 配置 OSS 客户端
	// 1. 加载默认配置
	// 2. 设置凭证提供器
	// 3. 设置区域为上海
	cfg := oss.LoadDefaultConfig().
		WithCredentialsProvider(credProvider).
		WithRegion("cn-shanghai")

	// 创建 OSS 客户端实例
	client := oss.NewClient(cfg)

	return &OssClient{client, bucket}
}

// UploadFile 上传文件到 OSS 存储桶
// 参数:
//   - ctx: 上下文，用于控制请求的生命周期
//   - objectKey: 对象键（文件在 OSS 中的路径）
//   - filePath: 要上传的本地文件路径
//   - bucket: 目标存储桶名称
//
// 返回:
//   - error: 如果上传过程中发生错误，返回相应的错误信息
func (o *OssClient) UploadFile(ctx context.Context, objectKey, filePath, bucket string) error {
	// 打开本地文件
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close() // 确保文件最终被关闭

	// 执行文件上传操作
	// PutObject 方法将文件内容上传到指定的存储桶和对象键位置
	_, err = o.PutObject(ctx, &oss.PutObjectRequest{
		Bucket: &bucket,
		Key:    &objectKey,
		Body:   file,
	})
	if err != nil {
		return fmt.Errorf("failed to upload file to OSS: %v", err)
	}

	// 打印上传成功信息
	fmt.Printf("File %s uploaded successfully to bucket %s as %s\n", filePath, bucket, objectKey)
	return nil
}
