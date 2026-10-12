// Package main 是 {{.ProjectName}} LLM Provider 模板。
//
// Provider 协议（公共面见 pkg/llm.go 的 Provider 接口）：
//   - Name()        返回 provider 名称
//   - Chat(ctx, req) 执行对话请求
//   - Close()       关闭清理资源
//
// 在 AgentPrimordia 中通过以下方式注册：
//
//	registry := llm.NewRegistry()
//	registry.Register("{{.ProjectName}}", myprovider.NewProvider(config))
package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Config 是 {{.ProjectName}} Provider 的配置（从 .ap.yaml 或 env 注入）。
type Config struct {
	APIKey   string        `yaml:"api_key"`
	Endpoint string        `yaml:"endpoint"`
	Model    string        `yaml:"model"`
	Timeout  time.Duration `yaml:"timeout"`
}

// DefaultConfig 返回默认值。
func DefaultConfig() Config {
	return Config{
		Endpoint: "https://api.{{.ProjectName}}.example.com",
		Model:    "{{.ProjectName}}-default",
		Timeout:  30 * time.Second,
	}
}

// Message 表示对话消息。
type Message struct {
	Role    string // system | user | assistant
	Content string
}

// ChatRequest 一次对话请求。
type ChatRequest struct {
	Model    string
	Messages []Message
	Stream   bool
}

// ChatResponse 一次对话响应。
type ChatResponse struct {
	Content      string
	FinishReason string
	Usage        Usage
}

// Usage token 用量。
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// ErrNotImplemented 表示 Provider 尚未实现的占位错误。
var ErrNotImplemented = errors.New("{{.ProjectName}}: not implemented")

// Provider 是 {{.ProjectName}} LLM Provider。
type Provider struct {
	mu  sync.RWMutex
	cfg Config
}

// NewProvider 构造 Provider。
func NewProvider(cfg Config) *Provider {
	def := DefaultConfig()
	if cfg.Endpoint == "" {
		cfg.Endpoint = def.Endpoint
	}
	if cfg.Model == "" {
		cfg.Model = def.Model
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = def.Timeout
	}
	return &Provider{cfg: cfg}
}

// Name 返回 Provider 唯一名称。
func (p *Provider) Name() string { return "{{.ProjectName}}" }

// Chat 执行对话（占位实现：接入真实 HTTP API 后替换）。
func (p *Provider) Chat(_ context.Context, req ChatRequest) (*ChatResponse, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.cfg.APIKey == "" {
		return nil, errors.New("api_key 不能为空")
	}
	if req.Model == "" {
		req.Model = p.cfg.Model
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("messages 不能为空")
	}

	// TODO: 替换为真实 HTTP 调用
	return nil, ErrNotImplemented
}

// Close 关闭 Provider（释放连接池等）。
func (p *Provider) Close() error { return nil }

// main 桩函数：保证 `go build ./...` 通过。
// 真实使用时由 ap 启动，并通过 llm.ProviderRegistry 注册到 AgentPrimordia。
func main() {
	fmt.Printf("{{.ProjectName}} provider. Use via `ap init` or import as library.\n")
}
