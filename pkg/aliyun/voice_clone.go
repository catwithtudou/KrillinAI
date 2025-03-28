package aliyun

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"krillin-ai/log"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// _encodeText 对文本进行URL编码，并按照阿里云API签名规范处理特殊字符
// 规范要求：
// 1. 对字符进行UTF-8编码
// 2. 将空格编码为%20，而不是+
// 3. 将星号(*)编码为%2A
// 4. 不对波浪线(~)进行编码
//
// @param text - 需要编码的原始文本
// @return string - 编码后的文本
func _encodeText(text string) string {
	encoded := url.QueryEscape(text)
	// 根据阿里云API签名规范替换特殊字符
	return strings.ReplaceAll(
		strings.ReplaceAll(
			strings.ReplaceAll(encoded, "+", "%20"),
			"*", "%2A"),
		"%7E", "~")
}

// _encodeDict 将参数字典转换为规范化的查询字符串
// 处理步骤：
// 1. 对参数名进行字典排序
// 2. 对参数名和参数值分别进行URL编码
// 3. 使用等号(=)连接编码后的参数名和参数值
// 4. 使用与号(&)连接所有参数对
//
// @param dic - 包含请求参数的字典
// @return string - 规范化的查询字符串
func _encodeDict(dic map[string]string) string {
	// 提取所有键并按字典序排序
	var keys []string
	for key := range dic {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// 构建规范化的参数对
	values := url.Values{}
	for _, k := range keys {
		values.Add(k, dic[k])
	}

	// 对整个查询字符串进行编码，并按规范处理特殊字符
	encodedText := values.Encode()
	return strings.ReplaceAll(
		strings.ReplaceAll(
			strings.ReplaceAll(encodedText, "+", "%20"),
			"*", "%2A"),
		"%7E", "~")
}

// GenerateSignature 生成阿里云API请求的签名
// 签名算法：
// 1. 使用请求参数构造规范化的字符串
// 2. 使用HMAC-SHA1算法对字符串进行加密
// 3. 将加密结果进行Base64编码
// 4. 对Base64编码结果进行URL编码
//
// @param secret - 访问密钥Secret（AccessKeySecret）
// @param stringToSign - 待签名的字符串，格式：HTTPMethod + "&" + 编码后的斜杠 + "&" + 编码后的参数字符串
// @return string - URL编码后的签名字符串
func GenerateSignature(secret, stringToSign string) string {
	// 在密钥末尾添加符号&
	key := []byte(secret + "&")
	data := []byte(stringToSign)

	// 使用HMAC-SHA1算法计算签名
	hash := hmac.New(sha1.New, key)
	hash.Write(data)

	// 对签名进行Base64编码
	signature := base64.StdEncoding.EncodeToString(hash.Sum(nil))

	// 对签名进行URL编码，确保可以安全传输
	return _encodeText(signature)
}

type VoiceCloneResp struct {
	RequestId string `json:"RequestId"`
	Message   string `json:"Message"`
	Code      int    `json:"Code"`
	VoiceName string `json:"VoiceName"`
}

type VoiceCloneClient struct {
	restyClient     *resty.Client
	accessKeyID     string
	accessKeySecret string
	appkey          string
}

func NewVoiceCloneClient(accessKeyID, accessKeySecret, appkey string) *VoiceCloneClient {
	return &VoiceCloneClient{
		restyClient:     resty.New(),
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		appkey:          appkey,
	}
}

// CosyVoiceClone 调用阿里云智能语音服务的音色克隆功能
// 该服务可以基于上传的音频样本，克隆出对应的声音特征，生成专属音色
//
// @param voicePrefix - 音色标识前缀，用于区分不同的音色克隆任务
// @param audioURL - 待克隆的音频样本URL地址
// @return string - 返回生成的音色ID（VoiceName）
// @return error - 处理过程中的错误信息
func (c *VoiceCloneClient) CosyVoiceClone(voicePrefix, audioURL string) (string, error) {
	log.GetLogger().Info("CosyVoiceClone请求开始", zap.String("voicePrefix", voicePrefix), zap.String("audioURL", audioURL))

	// 构建阿里云API请求参数
	// 参数说明：
	// - AccessKeyId: 访问密钥ID
	// - Action: API动作名称
	// - Format: 返回数据格式
	// - RegionId: 地域ID，目前音色克隆服务在上海地域
	// - SignatureMethod: 签名方法，使用HMAC-SHA1
	// - SignatureNonce: 唯一随机数，用于防止网络重放攻击
	// - SignatureVersion: 签名算法版本
	// - Timestamp: 请求时间戳（UTC格式）
	// - Version: API版本号
	// - VoicePrefix: 音色标识前缀
	// - Url: 音频样本的URL地址
	parameters := map[string]string{
		"AccessKeyId":      c.accessKeyID,
		"Action":           "CosyVoiceClone",
		"Format":           "JSON",
		"RegionId":         "cn-shanghai",
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureNonce":   uuid.New().String(),
		"SignatureVersion": "1.0",
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"Version":          "2019-08-19",
		"VoicePrefix":      voicePrefix,
		"Url":              audioURL,
	}

	// 生成规范化的请求字符串
	queryString := _encodeDict(parameters)

	// 构造待签名字符串
	// 格式：HTTPMethod + "&" + 编码后的斜杠 + "&" + 编码后的参数字符串
	stringToSign := "POST" + "&" + _encodeText("/") + "&" + _encodeText(queryString)

	// 使用AccessKeySecret生成请求签名
	signature := GenerateSignature(c.accessKeySecret, stringToSign)

	// 构建完整的请求URL
	// 阿里云智能语音服务的API端点：nls-slp.cn-shanghai.aliyuncs.com
	fullURL := fmt.Sprintf("https://nls-slp.cn-shanghai.aliyuncs.com/?Signature=%s&%s", signature, queryString)

	// 构建POST请求的表单数据
	values := url.Values{}
	for key, value := range parameters {
		values.Add(key, value)
	}

	// 发送HTTP请求并解析响应
	var res VoiceCloneResp
	resp, err := c.restyClient.R().SetResult(&res).Post(fullURL)
	if err != nil {
		log.GetLogger().Error("CosyVoiceClone post error", zap.Error(err))
		return "", fmt.Errorf("CosyVoiceClone post error: %w: ", err)
	}

	// 记录响应日志
	log.GetLogger().Info("CosyVoiceClone请求完毕", zap.String("Response", resp.String()))

	// 检查响应状态
	// 只有返回 "SUCCESS" 时才表示克隆成功
	if res.Message != "SUCCESS" {
		log.GetLogger().Error("CosyVoiceClone res message is not success",
			zap.String("Request Id", res.RequestId),
			zap.Int("Code", res.Code),
			zap.String("Message", res.Message))
		return "", fmt.Errorf("CosyVoiceClone res message is not success, message: %s", res.Message)
	}

	// 返回生成的音色ID
	return res.VoiceName, nil
}

func (c *VoiceCloneClient) CosyCloneList(voicePrefix string, pageIndex, pageSize int) {
	parameters := map[string]string{
		"AccessKeyId":      c.accessKeyID,
		"Action":           "ListCosyVoice",
		"Format":           "JSON",
		"RegionId":         "cn-shanghai",
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureNonce":   uuid.New().String(),
		"SignatureVersion": "1.0",
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"Version":          "2019-08-19",
		"VoicePrefix":      voicePrefix,
		"PageIndex":        fmt.Sprintf("%d", pageIndex),
		"PageSize":         fmt.Sprintf("%d", pageSize),
	}

	queryString := _encodeDict(parameters)
	stringToSign := "POST" + "&" + _encodeText("/") + "&" + _encodeText(queryString)
	signature := GenerateSignature(c.accessKeySecret, stringToSign)
	fullURL := fmt.Sprintf("https://nls-slp.cn-shanghai.aliyuncs.com/?Signature=%s&%s", signature, queryString)

	values := url.Values{}
	for key, value := range parameters {
		values.Add(key, value)
	}
	resp, err := c.restyClient.R().Post(fullURL)
	if err != nil {
		log.GetLogger().Error("CosyCloneList请求失败", zap.Error(err))
		return
	}
	log.GetLogger().Info("CosyCloneList请求成功", zap.String("Response", resp.String()))
}
