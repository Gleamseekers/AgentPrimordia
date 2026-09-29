// Stability: Stable — 多 Agent 并发调度。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/internal/pool"
)

// Pool 是多 Agent 并发调度器，支持任务分发、重试、取消和会话管理
type Pool = pool.Pool

// PoolConfig 是 Pool 的配置结构，包含最大并发数、超时、重试策略和默认 Agent 配置
type PoolConfig = pool.PoolConfig

// TaskConfig 是提交给 Pool 的任务配置，包含 ID、标题、提示词、会话和作用域等
type TaskConfig = pool.TaskConfig

// TaskResult 是任务执行结果，包含任务 ID、Agent 响应、错误、耗时和状态
type TaskResult = pool.TaskResult

// PoolEvent 是 Pool 发出的事件，包含事件类型、任务 ID、时间戳和附加数据
type PoolEvent = pool.PoolEvent

// PoolStats 是 Pool 的运行统计，包含总任务数、完成数、失败数、运行数等
type PoolStats = pool.PoolStats

// RetryPolicy 是任务重试策略，包含最大重试次数、退避时间和可重试错误模式
type RetryPolicy = pool.RetryPolicy

// ReActAgentConfig 是 Pool 中默认 Agent 的配置，包含系统提示词、最大轮次和温度
type ReActAgentConfig = pool.ReActAgentConfig

// PoolTaskStatus 表示 Pool 任务的状态（queued / running / completed / failed / cancelled）
type PoolTaskStatus = pool.PoolTaskStatus

// AgentFactory 是创建 Agent 实例的工厂函数，Pool 使用此工厂为每个任务创建 Agent
type AgentFactory = pool.AgentFactory

// AgentFactoryConfig 是创建 Agent 时传递的完整配置，包含名称、提示词、作用域和会话 ID
type AgentFactoryConfig = pool.AgentFactoryConfig

const (
	// PoolTaskQueued 表示任务已排队等待执行
	PoolTaskQueued = pool.PoolTaskQueued
	// PoolTaskRunning 表示任务正在执行
	PoolTaskRunning = pool.PoolTaskRunning
	// PoolTaskCompleted 表示任务已完成
	PoolTaskCompleted = pool.PoolTaskCompleted
	// PoolTaskFailed 表示任务执行失败
	PoolTaskFailed = pool.PoolTaskFailed
	// PoolTaskCancelled 表示任务已取消
	PoolTaskCancelled = pool.PoolTaskCancelled
)

// NewPool 创建多 Agent 并发调度器实例
var NewPool = pool.NewPool
