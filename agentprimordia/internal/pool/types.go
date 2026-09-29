package pool

import (
	"encoding/json"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

type PoolTaskStatus string

const (
	PoolTaskQueued    PoolTaskStatus = "queued"
	PoolTaskRunning   PoolTaskStatus = "running"
	PoolTaskCompleted PoolTaskStatus = "completed"
	PoolTaskFailed    PoolTaskStatus = "failed"
	PoolTaskCancelled PoolTaskStatus = "cancelled"
)

type TaskConfig struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Prompt     string            `json:"prompt"`
	SessionID  string            `json:"session_id,omitempty"`
	Tools      []tools.Tool      `json:"tools,omitempty"`
	FilesScope []string          `json:"files_scope,omitempty"`
	MaxTurns   int               `json:"max_turns,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type TaskResult struct {
	TaskID   string          `json:"task_id"`
	Task     TaskConfig      `json:"task"`
	Response *agent.Response `json:"response,omitempty"`
	Error    error           `json:"error,omitempty"`
	Duration time.Duration   `json:"duration"`
	Status   PoolTaskStatus  `json:"status"`
}

// MarshalJSON 自定义序列化：error 接口直接 JSON 序列化会输出 {}（无字段
// 可序列化），对外部消费者无意义。这里将 Error 转为其 Error() 字符串；
// 其余字段与原结构输出一致（外层 Error 字段遮蔽内嵌 alias 的同名字段）。
func (t TaskResult) MarshalJSON() ([]byte, error) {
	type alias TaskResult
	errStr := ""
	if t.Error != nil {
		errStr = t.Error.Error()
	}
	return json.Marshal(struct {
		alias
		Error string `json:"error,omitempty"`
	}{
		alias: alias(t),
		Error: errStr,
	})
}

type PoolEvent struct {
	Type      string    `json:"type"`
	TaskID    string    `json:"task_id"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data,omitempty"`
}

type PoolStats struct {
	TotalTasks        int `json:"total_tasks"`
	CompletedTasks    int `json:"completed_tasks"`
	FailedTasks       int `json:"failed_tasks"`
	RunningTasks      int `json:"running_tasks"`
	QueuedTasks       int `json:"queued_tasks"`
	MaxConcurrency    int `json:"max_concurrency"`
	ActiveConcurrency int `json:"active_concurrency"`
}

type PoolConfig struct {
	MaxConcurrency int           `json:"max_concurrency"`
	Timeout        time.Duration `json:"timeout"`
	RetryPolicy    RetryPolicy   `json:"retry_policy,omitempty"`
	// MaxRetainedTasks 保留的已完成任务数上限，超过时自动清理最早的终态任务（M8 修复）。
	// 0 表示不自动清理（向后兼容）。生产环境建议设置（如 1000）避免长期运行内存泄漏。
	MaxRetainedTasks int              `json:"max_retained_tasks,omitempty"`
	DefaultAgent     ReActAgentConfig `json:"default_agent"`
	// Task 9：动态 Agent 池（自动扩缩容）
	AutoScaler *AutoScalerConfig `json:"auto_scaler,omitempty"`
	// Phase 3 Task 4：可选的动态协程池配置（concurrency.GoroutinePool）。
	// 设置后 Pool 会创建一个内部 GoroutinePool 用于后台任务调度，并允许
	// 调用方通过 Pool.GoroutinePoolStats() 获取运行指标。
	GoroutinePool *GoroutinePoolConfig `json:"goroutine_pool,omitempty"`
}

// GoroutinePoolConfig 描述内部 GoroutinePool 的运行参数（Phase 3 Task 4）。
//
// Pool 创建时如果设置了 GoroutinePool，会在内部实例化一个
// concurrency.GoroutinePool 并用于 SubmitBackground 任务；底层协程池
// 的统计通过 Pool.GoroutinePoolStats() 暴露给 Prometheus。
type GoroutinePoolConfig struct {
	MinWorkers  int           `json:"min_workers"`
	MaxWorkers  int           `json:"max_workers"`
	QueueSize   int           `json:"queue_size"`
	IdleTimeout time.Duration `json:"idle_timeout"`
	// EnableLLMBatch（Phase 3 Task 6）：当 GoroutinePool 与 LLMBatch 同时启用时，
	// 通过协程池调度 BatchProcessor 的 flushLoop，减少阻塞主 Pool 调度线程。
	// 注意：开启此选项需要 Pool.SetModel 已被调用（BatchProcessor 需要 Provider）。
	EnableLLMBatch bool `json:"enable_llm_batch,omitempty"`
}

type RetryPolicy struct {
	MaxRetries      int           `json:"max_retries"`
	Backoff         time.Duration `json:"backoff"`
	RetryableErrors []string      `json:"retryable_errors"`
}

type ReActAgentConfig struct {
	SystemPrompt string  `json:"system_prompt"`
	MaxTurns     int     `json:"max_turns"`
	Temperature  float64 `json:"temperature"`
}

// AgentFactory 创建 Agent 实例的工厂函数
// Pool 使用此工厂为每个任务创建 Agent，确保 Agent 接收完整配置
type AgentFactory func(config AgentFactoryConfig) agent.Agent

// AgentFactoryConfig 是创建 Agent 时传递的完整配置
// 相比 ReActAgentConfig，包含 Memory/Scope/FileLock 等关键依赖
type AgentFactoryConfig struct {
	Name         string
	SystemPrompt string
	MaxTurns     int
	Temperature  float64
	FilesScope   []string
	SessionID    string
	Metadata     map[string]string
}
