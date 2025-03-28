package aliyun

import (
	"encoding/json"
	"fmt"
	"krillin-ai/log"
	"krillin-ai/pkg/util"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// TtsClient 阿里云语音合成客户端，用于将文本转换为语音
type TtsClient struct {
	AccessKeyID     string // 阿里云账号AccessKey ID
	AccessKeySecret string // 阿里云账号AccessKey Secret
	Appkey          string // 阿里云语音服务应用Appkey
}

// TtsHeader 语音合成WebSocket通信的消息头部结构
type TtsHeader struct {
	Appkey    string `json:"appkey"`     // 应用标识
	MessageID string `json:"message_id"` // 消息ID，用于标识请求
	TaskID    string `json:"task_id"`    // 任务ID，用于关联同一个合成任务的多个消息
	Namespace string `json:"namespace"`  // 命名空间，固定为"FlowingSpeechSynthesizer"
	Name      string `json:"name"`       // 消息名称，如StartSynthesis等
}

// StartSynthesisPayload 开始语音合成的请求参数
type StartSynthesisPayload struct {
	Voice                  string `json:"voice,omitempty"`                    // 发音人声音，如"xiaoyun"
	Format                 string `json:"format,omitempty"`                   // 音频格式，如"wav"、"mp3"
	SampleRate             int    `json:"sample_rate,omitempty"`              // 采样率，如8000、16000等
	Volume                 int    `json:"volume,omitempty"`                   // 音量，范围：0~100
	SpeechRate             int    `json:"speech_rate,omitempty"`              // 语速，范围：-500~500
	PitchRate              int    `json:"pitch_rate,omitempty"`               // 语调，范围：-500~500
	EnableSubtitle         bool   `json:"enable_subtitle,omitempty"`          // 是否启用字幕功能
	EnablePhonemeTimestamp bool   `json:"enable_phoneme_timestamp,omitempty"` // 是否启用音素时间戳
}

// RunSynthesisPayload 运行语音合成的请求参数
type RunSynthesisPayload struct {
	Text string `json:"text"` // 需要合成的文本内容
}

// Message WebSocket通信的消息结构
type Message struct {
	Header  TtsHeader   `json:"header"`            // 消息头部
	Payload interface{} `json:"payload,omitempty"` // 消息负载，根据不同消息类型可能有不同的结构
}

// NewTtsClient 创建新的语音合成客户端实例
func NewTtsClient(accessKeyId, accessKeySecret, appkey string) *TtsClient {
	return &TtsClient{
		AccessKeyID:     accessKeyId,
		AccessKeySecret: accessKeySecret,
		Appkey:          appkey,
	}
}

// Text2Speech 将文本转换为语音并保存到文件
// text: 需要合成的文本内容
// voice: 发音人声音
// outputFile: 输出音频文件路径
func (c *TtsClient) Text2Speech(text, voice, outputFile string) error {
	// 创建输出文件
	file, err := os.OpenFile(outputFile, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// 建立WebSocket连接
	var conn *websocket.Conn
	token, _ := CreateToken(c.AccessKeyID, c.AccessKeySecret) // 生成认证Token
	fullURL := "wss://nls-gateway-cn-beijing.aliyuncs.com/ws/v1?token=" + token
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second
	conn, _, err = dialer.Dial(fullURL, nil)
	if err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second * 60)) // 设置读取超时
	defer c.Close(conn)

	// 处理文本消息的回调函数
	onTextMessage := func(message string) {
		log.GetLogger().Info("Received text message", zap.String("Message", message))
	}

	// 处理二进制消息（音频数据）的回调函数
	onBinaryMessage := func(data []byte) {
		if _, err := file.Write(data); err != nil {
			log.GetLogger().Error("Failed to write data to file", zap.Error(err))
		}
	}

	// 用于同步的通道
	var (
		synthesisStarted  = make(chan struct{}) // 合成开始信号
		synthesisComplete = make(chan struct{}) // 合成完成信号
	)

	// 配置语音合成参数
	startPayload := StartSynthesisPayload{
		Voice:      voice,
		Format:     "wav",
		SampleRate: 44100,
		Volume:     50,
		SpeechRate: 0,
		PitchRate:  0,
	}

	// 启动消息接收协程
	go c.receiveMessages(conn, onTextMessage, onBinaryMessage, synthesisStarted, synthesisComplete)

	// 生成任务ID并开始语音合成
	taskId := util.GenerateID()
	log.GetLogger().Info("SpeechClient StartSynthesis", zap.String("taskId", taskId), zap.Any("payload", startPayload))
	if err := c.StartSynthesis(conn, taskId, startPayload, synthesisStarted); err != nil {
		return fmt.Errorf("failed to start synthesis: %w", err)
	}

	// 发送要合成的文本
	if err := c.RunSynthesis(conn, taskId, text); err != nil {
		return fmt.Errorf("failed to run synthesis: %w", err)
	}

	// 停止合成并等待完成
	if err := c.StopSynthesis(conn, taskId, synthesisComplete); err != nil {
		return fmt.Errorf("failed to stop synthesis: %w", err)
	}

	return nil
}

// sendMessage 发送WebSocket消息
// conn: WebSocket连接
// taskId: 任务ID
// name: 消息名称
// payload: 消息负载
func (c *TtsClient) sendMessage(conn *websocket.Conn, taskId, name string, payload interface{}) error {
	message := Message{
		Header: TtsHeader{
			Appkey:    c.Appkey,
			MessageID: util.GenerateID(),
			TaskID:    taskId,
			Namespace: "FlowingSpeechSynthesizer",
			Name:      name,
		},
		Payload: payload,
	}
	jsonData, _ := json.Marshal(message)
	log.GetLogger().Debug("SpeechClient sendMessage", zap.String("message", string(jsonData)))
	return conn.WriteJSON(message)
}

// StartSynthesis 开始语音合成
// conn: WebSocket连接
// taskId: 任务ID
// payload: 开始合成的参数
// synthesisStarted: 合成开始信号通道
func (c *TtsClient) StartSynthesis(conn *websocket.Conn, taskId string, payload StartSynthesisPayload, synthesisStarted chan struct{}) error {
	err := c.sendMessage(conn, taskId, "StartSynthesis", payload)
	if err != nil {
		return err
	}

	// 阻塞等待 SynthesisStarted 事件
	<-synthesisStarted

	return nil
}

// RunSynthesis 发送要合成的文本
// conn: WebSocket连接
// taskId: 任务ID
// text: 要合成的文本内容
func (c *TtsClient) RunSynthesis(conn *websocket.Conn, taskId, text string) error {
	return c.sendMessage(conn, taskId, "RunSynthesis", RunSynthesisPayload{Text: text})
}

// StopSynthesis 停止语音合成
// conn: WebSocket连接
// taskId: 任务ID
// synthesisComplete: 合成完成信号通道
func (c *TtsClient) StopSynthesis(conn *websocket.Conn, taskId string, synthesisComplete chan struct{}) error {
	err := c.sendMessage(conn, taskId, "StopSynthesis", nil)
	if err != nil {
		return err
	}

	// 阻塞等待 SynthesisCompleted 事件
	<-synthesisComplete

	return nil
}

// Close 关闭WebSocket连接
func (c *TtsClient) Close(conn *websocket.Conn) error {
	err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		return err
	}
	return conn.Close()
}

// receiveMessages 接收并处理WebSocket消息
// conn: WebSocket连接
// onTextMessage: 处理文本消息的回调函数
// onBinaryMessage: 处理二进制消息的回调函数
// synthesisStarted: 合成开始信号通道
// synthesisComplete: 合成完成信号通道
func (c *TtsClient) receiveMessages(conn *websocket.Conn, onTextMessage func(string), onBinaryMessage func([]byte), synthesisStarted, synthesisComplete chan struct{}) {
	defer close(synthesisComplete)
	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				log.GetLogger().Error("SpeechClient receiveMessages websocket非正常关闭", zap.Error(err))
				return
			}
			return
		}
		if messageType == websocket.TextMessage {
			var msg Message
			if err := json.Unmarshal(message, &msg); err != nil {
				log.GetLogger().Error("SpeechClient receiveMessages json解析失败", zap.Error(err))
				return
			}
			if msg.Header.Name == "SynthesisCompleted" {
				log.GetLogger().Info("SynthesisCompleted event received")
				// 收到结束消息退出
				break
			} else if msg.Header.Name == "SynthesisStarted" {
				log.GetLogger().Info("SynthesisStarted event received")
				close(synthesisStarted)
			} else {
				onTextMessage(string(message))
			}
		} else if messageType == websocket.BinaryMessage {
			onBinaryMessage(message)
		}
	}
}
