// Stability: Stable — 结构化日志。
package ap

import "github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/logger"

// Logger 结构化日志器
type Logger = logger.Logger

// LogConfig 日志配置
type LogConfig = logger.Config

// NewLogger 创建结构化日志器
var NewLogger = logger.New

const (
	// LogLevelDebug 调试级别
	LogLevelDebug = logger.LevelDebug
	// LogLevelInfo 信息级别
	LogLevelInfo = logger.LevelInfo
	// LogLevelWarn 警告级别
	LogLevelWarn = logger.LevelWarn
	// LogLevelError 错误级别
	LogLevelError = logger.LevelError

	// LogFormatJSON JSON 格式
	LogFormatJSON = logger.FormatJSON
	// LogFormatText 文本格式
	LogFormatText = logger.FormatText
)
