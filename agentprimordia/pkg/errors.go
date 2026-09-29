// Stability: Stable — 错误码与 sentinel 错误在 v1.0 前冻结，向后兼容。
package ap

import (
	"errors"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/concurrency"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/events"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/memory"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/persist"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/pool"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/security"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

// CodeError 是带错误码的错误类型，用于结构化错误返回
type CodeError struct {
	Code    string
	Message string
}

// GetCode 返回结构化错误码，使 GetErrorCode 的 errors.As 路径可命中
// （修复评估报告 §四.1-①：此前仅字段 Code、无访问器方法，errors.As 分支
// 永不命中，WithCode 构造的错误码经 GetErrorCode 只能返回 "UNKNOWN"。
// 注：因字段 Code 与同名方法冲突，访问器命名为 GetCode）。
func (e *CodeError) GetCode() string {
	return e.Code
}

// AggregatedError 聚合多个任务的错误信息，支持 errors.Is/errors.As 解包
type AggregatedError = pool.AggregatedError

// TaskError 单个任务的错误信息
type TaskError = pool.TaskError

// Error 返回 CodeError 的消息内容
func (e *CodeError) Error() string {
	return e.Message
}

// WithCode 创建带错误码的错误实例
func WithCode(code, message string) *CodeError {
	return &CodeError{Code: code, Message: message}
}

var (
	// ErrAgentStopped 表示 Agent 已被停止
	ErrAgentStopped = agent.ErrAgentStopped
	// ErrAgentRunning 表示 Agent 已在运行中
	ErrAgentRunning = errors.New("agent is already running")
	// ErrMaxTurnsExceeded 表示 Agent 超出最大推理轮次
	ErrMaxTurnsExceeded = agent.ErrMaxTurnsExceeded
	// ErrNoToolkit 表示 Agent 未配置tool包
	ErrNoToolkit = agent.ErrNoToolkit

	// ErrToolNotFound 表示请求的tool未注册
	ErrToolNotFound = tools.ErrToolNotFound
	// ErrToolExecution 表示tool执行失败
	ErrToolExecution = tools.ErrToolExecution
	// ErrConfirmDenied 表示tool确认被拒绝
	ErrConfirmDenied = tools.ErrConfirmDenied
	// ErrInvalidConfig 表示tool配置无效
	ErrInvalidConfig = tools.ErrInvalidConfig

	// ErrLLMCallFailed 表示 LLM 调用失败
	ErrLLMCallFailed = llm.ErrLLMCallFailed
	// ErrNotSupported 表示操作不被当前提供者支持
	ErrNotSupported = llm.ErrNotSupported
	// ErrCircuitOpen 表示熔断器已打开，请求被拒绝
	ErrCircuitOpen = llm.ErrCircuitOpen
	// ErrAPIKeyRequired 表示 API Key 未提供
	ErrAPIKeyRequired = llm.ErrAPIKeyRequired
	// ErrEmptyResponse 表示 LLM 返回空响应
	ErrEmptyResponse = llm.ErrEmptyResponse
	// ErrResponseParseFailed 表示 LLM 响应解析失败
	ErrResponseParseFailed = llm.ErrResponseParseFailed
	// ErrRetriesExhausted 表示所有重试已耗尽
	ErrRetriesExhausted = llm.ErrRetriesExhausted
	// ErrFallbackFailed 表示所有降级提供者均失败
	ErrFallbackFailed = llm.ErrFallbackFailed

	// ErrPoolFull 表示 Pool 已达最大容量
	// 注：当前 Pool 队列无界（Dispatch 不设排队上限），本 sentinel 为预留错误码，
	// 未来引入排队容量限制时作为抛出点（见评估报告 §四.1-④）。
	ErrPoolFull = errors.New("pool is at max capacity")
	// ErrTaskNotFound 表示任务未找到
	ErrTaskNotFound = pool.ErrTaskNotFound
	// ErrTimeout 表示操作超时
	ErrTimeout = pool.ErrTimeout

	// ErrContextCanceled 表示上下文已取消
	// re-export pool.ErrContextCanceled：executeTask 的 ctx 取消路径以 %w 抛出
	//（修复评估报告 §四.1-④：此前 pkg 本地 sentinel 无抛出点）。
	ErrContextCanceled = pool.ErrContextCanceled

	// ErrEpisodeNotFound 表示记忆片段未找到
	ErrEpisodeNotFound = memory.ErrEpisodeNotFound
	// ErrInvalidImportance 表示重要性值无效
	ErrInvalidImportance = memory.ErrInvalidImportance
	// ErrEmptyEpisodeID 表示记忆片段 ID 为空
	ErrEmptyEpisodeID = memory.ErrEmptyEpisodeID
	// ErrEmptySessionID 表示会话 ID 为空
	ErrEmptySessionID = memory.ErrEmptySessionID
	// ErrEmptyRole 表示角色为空
	ErrEmptyRole = memory.ErrEmptyRole
	// ErrEmptyContent 表示内容为空
	ErrEmptyContent = memory.ErrEmptyContent
	// ErrDimensionMismatch 表示向量维度不匹配
	ErrDimensionMismatch = memory.ErrDimensionMismatch
	// ErrVectorNotFound 表示向量记录未找到
	ErrVectorNotFound = memory.ErrVectorNotFound

	// ErrCommandBlocked 表示命令被安全策略阻止
	ErrCommandBlocked = security.ErrCommandBlocked
	// ErrCommandNotAllowed 表示命令不在允许列表中
	ErrCommandNotAllowed = security.ErrCommandNotAllowed
	// ErrAccessDenied 表示访问被拒绝
	ErrAccessDenied = security.ErrAccessDenied
	// ErrPathTraversal 表示检测到路径遍历攻击
	ErrPathTraversal = security.ErrPathTraversal

	// ErrBusClosed 表示事件总线已关闭
	ErrBusClosed = events.ErrBusClosed

	// ErrCheckpointNotFound 表示检查点未找到
	ErrCheckpointNotFound = persist.ErrCheckpointNotFound

	// ErrGlobalWriteConflict 表示全局写入冲突
	ErrGlobalWriteConflict = concurrency.ErrGlobalWriteConflict
	// ErrScopeOverlap 表示作用域存在重叠
	ErrScopeOverlap = concurrency.ErrScopeOverlap

	// --- Governance 错误 ---
	// ErrQuotaExceeded 表示配额已耗尽
	ErrQuotaExceeded = errors.New("ap: quota exceeded")
	// ErrPolicyDenied 表示策略执行被拒绝
	ErrPolicyDenied = errors.New("ap: policy enforcement denied")

	// --- Guardrail 错误 ---
	// ErrInputBlocked 表示输入被护栏拦截
	ErrInputBlocked = agent.ErrInputBlocked
	// ErrOutputBlocked 表示输出被护栏拦截
	// re-export agent.ErrOutputBlocked：guardrail 拦截路径抛出同一 sentinel
	//（修复评估报告 §四.1-③：此前 pkg 本地 errors.New，与拦截路径不匹配）。
	ErrOutputBlocked = agent.ErrOutputBlocked

	// --- Orchestration 错误 ---
	// ErrNoAgentAvailable 表示无可用 Agent 处理请求
	ErrNoAgentAvailable = errors.New("ap: no agent available")
	// ErrHandoffLimitExceeded 表示 Handoff 次数超限
	ErrHandoffLimitExceeded = errors.New("ap: handoff limit exceeded")

	// --- A2A 错误 ---
	// ErrA2ATaskNotFound 表示 A2A 任务未找到
	ErrA2ATaskNotFound = errors.New("ap: a2a task not found")
	// ErrA2ATaskConflict 表示 A2A 任务冲突
	ErrA2ATaskConflict = errors.New("ap: a2a task conflict")

	// --- Provider 超时/速率 ---
	// ErrRateLimited 表示 LLM 请求被速率限制
	ErrRateLimited = errors.New("ap: rate limited")
	// ErrProviderTimeout 表示 LLM 提供者超时
	ErrProviderTimeout = errors.New("ap: provider timeout")
)

// GetErrorCode 从错误中提取结构化错误码，支持 sentinel 错误和 Code() 接口
// errorCodeMapping 将 sentinel 错误映射到结构化错误码，按模块分组
var errorCodeMapping = map[error]string{
	// --- Agent 错误 ---
	ErrAgentStopped:     "AGENT_001",
	ErrAgentRunning:     "AGENT_002",
	ErrMaxTurnsExceeded: "AGENT_003",
	ErrNoToolkit:        "AGENT_004",

	// --- Tool 错误 ---
	ErrToolNotFound:  "TOOL_001",
	ErrToolExecution: "TOOL_002",
	ErrInvalidConfig: "TOOL_003",
	ErrConfirmDenied: "TOOL_004",

	// --- LLM 错误 ---
	ErrLLMCallFailed:       "LLM_001",
	ErrNotSupported:        "LLM_002",
	ErrCircuitOpen:         "LLM_003",
	ErrAPIKeyRequired:      "LLM_004",
	ErrEmptyResponse:       "LLM_005",
	ErrResponseParseFailed: "LLM_006",
	ErrRetriesExhausted:    "LLM_007",
	ErrFallbackFailed:      "LLM_008",

	// --- Pool 错误 ---
	ErrPoolFull:     "POOL_001",
	ErrTaskNotFound: "POOL_002",
	ErrTimeout:      "POOL_003",

	// --- Context 错误 ---
	ErrContextCanceled: "CTX_001",

	// --- Memory 错误 ---
	ErrEpisodeNotFound:   "MEM_001",
	ErrInvalidImportance: "MEM_002",
	ErrEmptyEpisodeID:    "MEM_003",
	ErrEmptySessionID:    "MEM_004",
	ErrEmptyRole:         "MEM_005",
	ErrEmptyContent:      "MEM_006",
	ErrDimensionMismatch: "MEM_007",
	ErrVectorNotFound:    "MEM_008",

	// --- Security 错误 ---
	ErrCommandBlocked:    "SEC_001",
	ErrCommandNotAllowed: "SEC_002",
	ErrAccessDenied:      "SEC_003",
	ErrPathTraversal:     "SEC_004",

	// --- Event 错误 ---
	ErrBusClosed: "EVT_001",

	// --- Persistence 错误 ---
	ErrCheckpointNotFound: "PST_001",

	// --- Concurrency 错误 ---
	ErrGlobalWriteConflict: "CON_001",
	ErrScopeOverlap:        "CON_002",

	// --- Governance 错误 ---
	ErrQuotaExceeded: "GOV_001",
	ErrPolicyDenied:  "GOV_002",
	ErrOutputTooLong: "GOV_003",

	// --- Guardrail 错误 ---
	ErrInputBlocked:  "GRD_001",
	ErrOutputBlocked: "GRD_002",

	// --- Orchestration 错误 ---
	ErrNoAgentAvailable:     "ORC_001",
	ErrHandoffLimitExceeded: "ORC_002",

	// --- A2A 错误 ---
	ErrA2ATaskNotFound: "A2A_001",
	ErrA2ATaskConflict: "A2A_002",

	// --- Provider 错误 ---
	ErrRateLimited:     "LLM_009",
	ErrProviderTimeout: "LLM_010",
}

func GetErrorCode(err error) string {
	type coded interface{ GetCode() string }
	var c coded
	if errors.As(err, &c) {
		return c.GetCode()
	}

	for sentinel, code := range errorCodeMapping {
		if errors.Is(err, sentinel) {
			return code
		}
	}

	return "UNKNOWN"
}

// IsRetryable 判断错误是否可重试（超时、速率限制、熔断器打开）
func IsRetryable(err error) bool {
	return errors.Is(err, ErrProviderTimeout) ||
		errors.Is(err, ErrRateLimited) ||
		errors.Is(err, ErrCircuitOpen) ||
		errors.Is(err, ErrRetriesExhausted)
}

// IsTimeout 判断错误是否为超时类错误
func IsTimeout(err error) bool {
	return errors.Is(err, ErrProviderTimeout) ||
		errors.Is(err, ErrTimeout) ||
		errors.Is(err, ErrContextCanceled)
}

// IsAccessDenied 判断错误是否为访问拒绝类错误
func IsAccessDenied(err error) bool {
	return errors.Is(err, ErrAccessDenied) ||
		errors.Is(err, ErrCommandBlocked) ||
		errors.Is(err, ErrCommandNotAllowed) ||
		errors.Is(err, ErrPolicyDenied) ||
		errors.Is(err, ErrInputBlocked) ||
		errors.Is(err, ErrOutputBlocked)
}
