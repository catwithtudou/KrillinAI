package aliyun

import (
	"encoding/json"
	"fmt"
	"io"
	"krillin-ai/internal/storage"
	"krillin-ai/internal/types"
	"krillin-ai/log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// AsrClient 阿里云语音识别客户端结构体
// BailianApiKey 为阿里云百炼API的访问密钥
type AsrClient struct {
	BailianApiKey string
}

// NewAsrClient 创建新的语音识别客户端实例
func NewAsrClient(bailianApiKey string) *AsrClient {
	return &AsrClient{
		BailianApiKey: bailianApiKey,
	}
}

const (
	// wsURL WebSocket服务器地址，用于与阿里云ASR服务建立连接
	wsURL = "wss://dashscope.aliyuncs.com/api-ws/v1/inference/"
)

// dialer WebSocket连接器，使用默认配置
var dialer = websocket.DefaultDialer

// Transcription 执行语音转写任务
// audioFile: 音频文件路径
// language: 识别的目标语言
// workDir: 工作目录
// 返回转写结果数据和可能的错误
func (c AsrClient) Transcription(audioFile, language, workDir string) (*types.TranscriptionData, error) {
	// 预处理音频文件：转换为单声道、16kHz采样率的格式
	processedAudioFile, err := processAudio(audioFile)
	if err != nil {
		log.GetLogger().Error("处理音频失败", zap.Error(err), zap.String("audio file", audioFile))
		return nil, err
	}

	// 建立WebSocket连接
	conn, err := connectWebSocket(c.BailianApiKey)
	if err != nil {
		log.GetLogger().Error("连接WebSocket失败", zap.Error(err), zap.String("audio file", audioFile))
		return nil, err
	}
	defer closeConnection(conn)

	// 创建用于任务状态同步的通道
	taskStarted := make(chan bool)
	taskDone := make(chan bool)

	// 初始化结果存储
	words := make([]types.Word, 0)
	text := ""
	// 启动异步结果接收器
	startResultReceiver(conn, &words, &text, taskStarted, taskDone)

	// 发送run-task指令
	taskID, err := sendRunTaskCmd(conn, language)
	if err != nil {
		log.GetLogger().Error("发送run-task指令失败", zap.Error(err), zap.String("audio file", audioFile))
	}

	// 等待task-started事件
	waitForTaskStarted(taskStarted)

	// 发送待识别音频文件流
	if err := sendAudioData(conn, processedAudioFile); err != nil {
		log.GetLogger().Error("发送音频数据失败", zap.Error(err))
	}

	// 发送finish-task指令
	if err := sendFinishTaskCmd(conn, taskID); err != nil {
		log.GetLogger().Error("发送finish-task指令失败", zap.Error(err), zap.String("audio file", audioFile))
	}

	// 等待任务完成或失败
	<-taskDone

	if len(words) == 0 {
		log.GetLogger().Info("识别结果为空", zap.String("audio file", audioFile))
	}
	log.GetLogger().Debug("识别结果", zap.Any("words", words), zap.String("text", text), zap.String("audio file", audioFile))

	transcriptionData := &types.TranscriptionData{
		Text:  text,
		Words: words,
	}

	return transcriptionData, nil
}

// AsrHeader WebSocket通信的消息头部结构
type AsrHeader struct {
	Action       string                 `json:"action"`                  // 操作类型
	TaskID       string                 `json:"task_id"`                 // 任务ID
	Streaming    string                 `json:"streaming"`               // 流式处理标识
	Event        string                 `json:"event"`                   // 事件类型
	ErrorCode    string                 `json:"error_code,omitempty"`    // 错误代码
	ErrorMessage string                 `json:"error_message,omitempty"` // 错误信息
	Attributes   map[string]interface{} `json:"attributes"`              // 附加属性
}

// Output 识别结果输出结构
type Output struct {
	Sentence struct {
		BeginTime int64  `json:"begin_time"` // 句子开始时间
		EndTime   *int64 `json:"end_time"`   // 句子结束时间
		Text      string `json:"text"`       // 识别文本
		Words     []struct {
			BeginTime   int64  `json:"begin_time"`  // 单词开始时间
			EndTime     *int64 `json:"end_time"`    // 单词结束时间
			Text        string `json:"text"`        // 单词文本
			Punctuation string `json:"punctuation"` // 标点符号
		} `json:"words"`
	} `json:"sentence"`
	Usage interface{} `json:"usage"` // 用量统计
}

type Payload struct {
	TaskGroup  string     `json:"task_group"`
	Task       string     `json:"task"`
	Function   string     `json:"function"`
	Model      string     `json:"model"`
	Parameters Params     `json:"parameters"`
	Resources  []Resource `json:"resources"`
	Input      Input      `json:"input"`
	Output     Output     `json:"output,omitempty"`
}

type Params struct {
	Format                   string   `json:"format"`
	SampleRate               int      `json:"sample_rate"`
	VocabularyID             string   `json:"vocabulary_id"`
	DisfluencyRemovalEnabled bool     `json:"disfluency_removal_enabled"`
	LanguageHints            []string `json:"language_hints"`
}

type Resource struct {
	ResourceID   string `json:"resource_id"`
	ResourceType string `json:"resource_type"`
}

type Input struct {
}

type Event struct {
	Header  AsrHeader `json:"header"`
	Payload Payload   `json:"payload"`
}

// processAudio 处理音频文件
// 将输入音频转换为ASR服务要求的格式：单声道、16kHz采样率
func processAudio(filePath string) (string, error) {
	dest := strings.ReplaceAll(filePath, filepath.Ext(filePath), "_mono_16K.mp3")
	cmdArgs := []string{"-i", filePath, "-ac", "1", "-ar", "16000", "-b:a", "192k", dest}
	cmd := exec.Command(storage.FfmpegPath, cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.GetLogger().Error("处理音频失败", zap.Error(err), zap.String("audio file", filePath), zap.String("output", string(output)))
		return "", err
	}
	return dest, nil
}

// connectWebSocket 建立WebSocket连接
// 使用API密钥进行认证，并设置必要的请求头
func connectWebSocket(apiKey string) (*websocket.Conn, error) {
	header := make(http.Header)
	header.Add("X-DashScope-DataInspection", "enable")
	header.Add("Authorization", fmt.Sprintf("bearer %s", apiKey))
	conn, _, err := dialer.Dial(wsURL, header)
	return conn, err
}

// startResultReceiver 启动异步结果接收处理
// 持续监听WebSocket连接，处理服务器返回的识别结果
func startResultReceiver(conn *websocket.Conn, words *[]types.Word, text *string, taskStarted chan<- bool, taskDone chan<- bool) {
	go func() {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.GetLogger().Error("解析服务器消息失败：", zap.Error(err))
				continue
			}
			currentEvent := Event{}
			err = json.Unmarshal(message, &currentEvent)
			if err != nil {
				log.GetLogger().Error("解析服务器消息失败：", zap.Error(err))
				continue
			}
			if currentEvent.Payload.Output.Sentence.EndTime != nil {
				// 本句结束，添加当前的words和text
				*text += currentEvent.Payload.Output.Sentence.Text
				currentNum := 0
				if len(*words) > 0 {
					currentNum = (*words)[len(*words)-1].Num + 1
				}
				for _, word := range currentEvent.Payload.Output.Sentence.Words {
					*words = append(*words, types.Word{
						Num:   currentNum,
						Text:  strings.TrimSpace(word.Text), // 阿里云这边的word后面会有空格
						Start: float64(word.BeginTime) / 1000,
						End:   float64(*word.EndTime) / 1000,
					})
					currentNum++
				}
			}
			if handleEvent(conn, &currentEvent, taskStarted, taskDone) {
				return
			}
		}
	}()
}

// sendRunTaskCmd 发送任务启动命令
// 生成并发送任务初始化指令
func sendRunTaskCmd(conn *websocket.Conn, language string) (string, error) {
	runTaskCmd, taskID, err := generateRunTaskCmd(language)
	if err != nil {
		return "", err
	}
	err = conn.WriteMessage(websocket.TextMessage, []byte(runTaskCmd))
	return taskID, err
}

// 生成run-task指令
func generateRunTaskCmd(language string) (string, string, error) {
	taskID := uuid.New().String()
	runTaskCmd := Event{
		Header: AsrHeader{
			Action:    "run-task",
			TaskID:    taskID,
			Streaming: "duplex",
		},
		Payload: Payload{
			TaskGroup: "audio",
			Task:      "asr",
			Function:  "recognition",
			Model:     "paraformer-realtime-v2",
			Parameters: Params{
				Format:        "mp3",
				SampleRate:    16000,
				LanguageHints: []string{language},
			},
			Input: Input{},
		},
	}
	runTaskCmdJSON, err := json.Marshal(runTaskCmd)
	return string(runTaskCmdJSON), taskID, err
}

// 等待task-started事件
func waitForTaskStarted(taskStarted chan bool) {
	select {
	case <-taskStarted:
		log.GetLogger().Info("阿里云语音识别任务开启成功")
	case <-time.After(10 * time.Second):
		log.GetLogger().Error("等待task-started超时，任务开启失败")
	}
}

// 发送音频数据
func sendAudioData(conn *websocket.Conn, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	buf := make([]byte, 1024) // 100ms的音频大约1024字节
	for {
		n, err := file.Read(buf)
		if n == 0 {
			break
		}
		if err != nil && err != io.EOF {
			return err
		}
		err = conn.WriteMessage(websocket.BinaryMessage, buf[:n])
		if err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// 发送finish-task指令
func sendFinishTaskCmd(conn *websocket.Conn, taskID string) error {
	finishTaskCmd, err := generateFinishTaskCmd(taskID)
	if err != nil {
		return err
	}
	err = conn.WriteMessage(websocket.TextMessage, []byte(finishTaskCmd))
	return err
}

// 生成finish-task指令
func generateFinishTaskCmd(taskID string) (string, error) {
	finishTaskCmd := Event{
		Header: AsrHeader{
			Action:    "finish-task",
			TaskID:    taskID,
			Streaming: "duplex",
		},
		Payload: Payload{
			Input: Input{},
		},
	}
	finishTaskCmdJSON, err := json.Marshal(finishTaskCmd)
	return string(finishTaskCmdJSON), err
}

// handleEvent 处理WebSocket事件
// 根据不同的事件类型执行相应的处理逻辑
func handleEvent(conn *websocket.Conn, event *Event, taskStarted chan<- bool, taskDone chan<- bool) bool {
	switch event.Header.Event {
	case "task-started":
		log.GetLogger().Info("收到task-started事件", zap.String("taskID", event.Header.TaskID))
		taskStarted <- true
	case "result-generated":
		log.GetLogger().Info("收到result-generated事件", zap.String("当前text", event.Payload.Output.Sentence.Text))
	case "task-finished":
		log.GetLogger().Info("收到task-finished事件，任务完成", zap.String("taskID", event.Header.TaskID))
		taskDone <- true
		return true
	case "task-failed":
		log.GetLogger().Info("收到task-failed事件", zap.String("taskID", event.Header.TaskID))
		handleTaskFailed(event, conn)
		taskDone <- true
		return true
	default:
		log.GetLogger().Info("未知事件：", zap.String("event", event.Header.Event))
	}
	return false
}

// handleTaskFailed 处理任务失败事件
func handleTaskFailed(event *Event, conn *websocket.Conn) {
	log.GetLogger().Error("任务失败：", zap.String("error", event.Header.ErrorMessage))
}

// closeConnection 安全关闭WebSocket连接
func closeConnection(conn *websocket.Conn) {
	if conn != nil {
		conn.Close()
	}
}
