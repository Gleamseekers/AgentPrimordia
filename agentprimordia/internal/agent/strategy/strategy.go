// Package strategy 实现 v5.2 认知引擎架构进化：策略驱动的认知内核。
//
// 从「单一 ReAct 循环」到「策略即一等公民」——推理策略可插拔、可运行时
// 热切换、可 A/B 对照。本包为新增式隔离层：通过 Engine 原语接口消费底层
// 能力，零改动 react_loop 热路径（回滚 = 移除包引用，见 docs/v5.2重构风险评审.md）。
package strategy

import (
	"context"
	"fmt"
	"sync"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

// Engine 引擎原语接口（v5.2 冻结点）：策略驱动底层能力的最小依赖面。
type Engine interface {
	// Complete 单轮 LLM 补全
	Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error)
	// ExecuteTool 执行工具并返回结果文本
	ExecuteTool(ctx context.Context, name, args string) (string, error)
}

// Task 策略执行任务
type Task struct {
	Goal         string            // 目标/用户输入
	SystemPrompt string            // 系统提示词
	History      []llm.ChatMessage // 既有对话历史
	MaxTurns     int               // 最大轮数（<=0 用策略默认）
}

// Result 策略执行结果
type Result struct {
	Output       string              // 最终输出
	Turns        int                 // 实际消耗轮数
	Usage        llm.Usage           // token 用量
	Verification *VerificationReport // 验证报告（未验证为 nil）
}

// Strategy 推理策略接口（v5.2 冻结点）：实现即插件。
type Strategy interface {
	// Name 策略唯一名（注册与热切换的键）
	Name() string
	// Run 执行任务
	Run(ctx context.Context, eng Engine, task Task) (*Result, error)
}

// Registry 策略注册表：注册 / 取用 / 默认策略热切换（并发安全）。
type Registry struct {
	mu       sync.RWMutex
	strats   map[string]Strategy
	default_ string
}

// NewRegistry 创建空注册表
func NewRegistry() *Registry {
	return &Registry{strats: make(map[string]Strategy)}
}

// Register 注册策略；重名覆盖（便于测试替换实现）
func (r *Registry) Register(s Strategy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.strats[s.Name()] = s
}

// Get 按名取策略；未注册返回错误
func (r *Registry) Get(name string) (Strategy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.strats[name]
	if !ok {
		return nil, fmt.Errorf("strategy: 未注册的策略 %q", name)
	}
	return s, nil
}

// SetDefault 设置默认策略；策略须已注册
func (r *Registry) SetDefault(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.strats[name]; !ok {
		return fmt.Errorf("strategy: 无法设默认：策略 %q 未注册", name)
	}
	r.default_ = name
	return nil
}

// Default 返回默认策略；未设置返回错误（杜绝静默回退）
func (r *Registry) Default() (Strategy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.default_ == "" {
		return nil, fmt.Errorf("strategy: 未设置默认策略")
	}
	s := r.strats[r.default_]
	return s, nil
}

// Names 返回已注册策略名列表
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.strats))
	for n := range r.strats {
		names = append(names, n)
	}
	return names
}
