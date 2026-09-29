// Stability: Experimental — v3.6.0 新增多模态实时能力，API 可能随使用场景演进而调整。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/realtime"
)

// --- 核心类型导出 ---

// RealtimeHub 实时会话编排器
type RealtimeHub = realtime.RealtimeHub

// RealtimeSession 实时会话
type RealtimeSession = realtime.Session

// SessionState 会话状态
type RealtimeSessionState = realtime.SessionState

// ASRAdapter 语音识别适配器接口
type ASRAdapter = realtime.ASRAdapter

// TTSAdapter 语音合成适配器接口
type TTSAdapter = realtime.TTSAdapter

// HubConfig 实时会话编排配置
type RealtimeHubConfig = realtime.HubConfig

// RealtimeRuntime 实时运行时：装配 Hub + 感知模块 + 事件总线 + 清理器
type RealtimeRuntime = realtime.Runtime

// RealtimeRuntimeConfig 实时运行时配置
type RealtimeRuntimeConfig = realtime.RuntimeConfig

// RealtimeEvent 实时事件
type RealtimeEvent = realtime.RealtimeEvent

// RealtimeEventType 实时事件类型
type RealtimeEventType = realtime.RealtimeEventType

// RealtimeEventBus 实时事件总线（供 UI/监控消费）
type RealtimeEventBus = realtime.EventBus

// RealtimeBargeInHandler 打断处理器
type RealtimeBargeInHandler = realtime.BargeInHandler

// RealtimeFusedInput 融合后的多模态输入
type RealtimeFusedInput = realtime.FusedInput

// --- 事件类型常量导出 ---

const (
	// RealtimeEventSessionCreated 会话创建
	RealtimeEventSessionCreated = realtime.EventSessionCreated
	// RealtimeEventSessionClosed 会话关闭
	RealtimeEventSessionClosed = realtime.EventSessionClosed
	// RealtimeEventStateChange 状态变更
	RealtimeEventStateChange = realtime.EventStateChange
	// RealtimeEventAudioReceived 音频接收
	RealtimeEventAudioReceived = realtime.EventAudioReceived
	// RealtimeEventTranscriptionReady 转写就绪
	RealtimeEventTranscriptionReady = realtime.EventTranscriptionReady
	// RealtimeEventResponseReady 响应就绪
	RealtimeEventResponseReady = realtime.EventResponseReady
	// RealtimeEventBargeIn 打断
	RealtimeEventBargeIn = realtime.EventBargeIn
	// RealtimeEventError 错误
	RealtimeEventError = realtime.EventError
)

// --- 状态常量导出 ---

const (
	// SessionIdle 空闲
	RealtimeIdle = realtime.SessionIdle
	// SessionListening 监听中
	RealtimeListening = realtime.SessionListening
	// SessionThinking 思考中
	RealtimeThinking = realtime.SessionThinking
	// SessionSpeaking 表达中
	RealtimeSpeaking = realtime.SessionSpeaking
)

// --- 构造器导出 ---

var (
	// NewRealtimeHub 创建实时会话编排器
	NewRealtimeHub = realtime.NewRealtimeHub
	// NewRealtimeSession 创建实时会话
	NewRealtimeSession = realtime.NewSession
	// NewRealtimeRuntime 创建实时运行时（v4.1：真实 ASR/TTS 可注入 RuntimeConfig）
	NewRealtimeRuntime = realtime.NewRuntime
	// NewOpenAIASR 创建真实 ASR 适配器（v4.1：OpenAI Whisper 兼容端点，本地 faster-whisper 免 key）
	NewOpenAIASR = realtime.NewOpenAIASR
	// NewOpenAITTS 创建真实 TTS 适配器（v4.1：OpenAI TTS 兼容端点，本地 Piper/edge-tts 免 key）
	NewOpenAITTS = realtime.NewOpenAITTS
)

// --- 真实适配器选项（v4.1） ---

var (
	// WithASRModel 设置 ASR 模型名（默认 whisper-1）
	WithASRModel = realtime.WithASRModel
	// WithASRHTTPClient 设置 ASR 自定义 HTTP 客户端
	WithASRHTTPClient = realtime.WithASRHTTPClient
	// WithTTSModel 设置 TTS 模型名（默认 tts-1）
	WithTTSModel = realtime.WithTTSModel
	// WithTTSVoice 设置 TTS 发音人（默认 alloy）
	WithTTSVoice = realtime.WithTTSVoice
	// WithTTSHTTPClient 设置 TTS 自定义 HTTP 客户端
	WithTTSHTTPClient = realtime.WithTTSHTTPClient
)
