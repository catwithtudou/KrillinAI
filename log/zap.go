package log

import (
	"os" // 导入操作系统功能，用于文件操作

	"go.uber.org/zap"         // 导入Uber开源的高性能日志库zap
	"go.uber.org/zap/zapcore" // 导入zap的核心组件，用于自定义日志配置
)

// Logger 全局日志对象，提供给整个应用程序使用
var Logger *zap.Logger

// InitLogger 初始化日志系统
// 配置了两个输出目标：
// 1. JSON格式输出到app.log文件（调试级别）
// 2. 控制台格式输出到终端（信息级别）
func InitLogger() {
	// 创建或打开日志文件，使用追加模式
	file, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		panic("无法打开日志文件: " + err.Error()) // 如果无法创建日志文件则终止程序
	}

	// 创建文件输出同步器
	fileSyncer := zapcore.AddSync(file)
	// 创建控制台输出同步器
	consoleSyncer := zapcore.AddSync(os.Stdout)

	// 使用生产环境的编码器配置
	encoderConfig := zap.NewProductionEncoderConfig()
	// 自定义时间格式为ISO8601标准格式（YYYY-MM-DDThh:mm:ss±hh:mm）
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	// 创建多输出的日志核心
	// 使用zapcore.NewTee可以将日志同时输出到多个目标
	core := zapcore.NewTee(
		zapcore.NewCore(zapcore.NewJSONEncoder(encoderConfig), fileSyncer, zap.DebugLevel),      // 写入文件（JSON 格式），记录Debug及以上级别
		zapcore.NewCore(zapcore.NewConsoleEncoder(encoderConfig), consoleSyncer, zap.InfoLevel), // 输出到终端，记录Info及以上级别
	)

	// 创建Logger实例，并添加调用者信息（文件名和行号）
	Logger = zap.New(core, zap.AddCaller())
}

// GetLogger 获取全局日志对象的方法
// 返回已初始化的zap.Logger实例，供应用程序各部分使用
func GetLogger() *zap.Logger {
	return Logger
}
