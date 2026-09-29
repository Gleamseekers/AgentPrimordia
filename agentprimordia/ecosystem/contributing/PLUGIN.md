# 插件贡献指南

## 快速开始

1. 在 `ecosystem/plugins/` 下创建新目录
2. 实现 `tools.ToolPlugin` 接口（Name/Version/Tools/Init/Close）
3. 每个工具实现 `tools.Tool` 接口（Name/Description/Parameters/Execute）
4. 在 `registry.json` 中注册插件元数据
5. 编写测试

## ToolPlugin 接口

```go
type ToolPlugin interface {
    Name() string
    Version() string
    Tools() []Tool
    Init(config map[string]any) error
    Close() error
}
```

## Tool 接口

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() json.RawMessage
    Execute(ctx context.Context, args json.RawMessage) (*Result, error)
}
```

## 命名规范

- 目录名：小写，与 stdlib 冲突时加 `plugin` 后缀（如 `sqlplugin`）
- 文件名：`plugin.go`
- 测试文件：`plugin_test.go`

## 注册到索引

在 `registry.json` 的 `plugins` 数组中添加：
```json
{
  "name": "your-plugin",
  "version": "0.1.0",
  "description": "插件描述",
  "category": "分类",
  "import_path": "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/ecosystem/plugins/your-plugin",
  "tools": ["tool_name"],
  "tags": ["tag1", "tag2"]
}
```
