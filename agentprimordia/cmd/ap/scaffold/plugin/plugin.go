// Package main 是 {{.ProjectName}} 插件的入口。
//
// 插件协议（参见 internal/tools/types.go）：
//   - Name()        返回插件唯一名称
//   - Version()     返回 SemVer 版本字符串
//   - Tools()       返回插件提供的工具列表
//   - Init(config)  用 map[string]any 形式的配置初始化
//   - Close()       关闭插件时清理资源
//
// 在 AgentPrimordia 中通过以下方式加载：
//
//	loader := ap.NewPluginLoader(registry)
//	err := loader.Load(myplugin.New(config))
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

// Plugin 是 {{.ProjectName}} 插件实现。
type Plugin struct {
	name    string
	version string
	mu      sync.RWMutex
	cfg     map[string]any
}

// New 构造插件实例（不读取 config，配置在 Init 中传入）。
func New() *Plugin {
	return &Plugin{
		name:    "{{.ProjectName}}",
		version: "0.1.0",
	}
}

// Name 返回插件名称。
func (p *Plugin) Name() string { return p.name }

// Version 返回插件版本。
func (p *Plugin) Version() string { return p.version }

// Tools 返回插件注册的工具集合。
func (p *Plugin) Tools() []ap.Tool {
	return []ap.Tool{
		NewEchoTool(),
	}
}

// Init 初始化插件：读取 config 中的必要字段。
func (p *Plugin) Init(config map[string]any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cfg = config
	// 在此校验 config 必填字段并初始化资源（例如打开连接、初始化缓存等）。
	return nil
}

// Close 关闭插件。
func (p *Plugin) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cfg = nil
	return nil
}

// EchoTool 是一个最小可运行的工具样例：返回输入字符串。
//
// 真实插件通常会把工具拆分到独立文件（例如 tools.go），并在 Plugin.Tools 中
// 注册多个工具。
type EchoTool struct{}

// NewEchoTool 构造 Echo 工具。
func NewEchoTool() *EchoTool { return &EchoTool{} }

// Name 工具名称。
func (t *EchoTool) Name() string { return "echo" }

// Description 工具描述。
func (t *EchoTool) Description() string { return "回显输入字符串（用于演示插件工具）" }

// Parameters 工具参数 JSON Schema。
func (t *EchoTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "text": {"type": "string", "description": "要回显的文本"}
  },
  "required": ["text"]
}`)
}

// Execute 执行工具。
func (t *EchoTool) Execute(_ context.Context, args json.RawMessage) (*tools.Result, error) {
	var in struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("echo: 解析参数失败: %w", err)
	}
	if in.Text == "" {
		return nil, fmt.Errorf("echo: 参数 text 不能为空")
	}
	out, _ := json.Marshal(map[string]any{"echo": in.Text})
	return tools.NewResult(string(out)), nil
}

// main 桩函数：仅保证 `go build ./...` 通过（模板本身被 -buildmode=plugin 构建时为 .so）。
func main() {}
