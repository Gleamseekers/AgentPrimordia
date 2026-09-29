package git

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

// Plugin 是 Git 版本控制操作插件
type Plugin struct {
	tool *GitTool
}

// New 创建新的 Git 插件实例
func New() *Plugin {
	return &Plugin{tool: &GitTool{}}
}

// Name 返回插件名称
func (p *Plugin) Name() string { return "git" }

// Version 返回插件版本
func (p *Plugin) Version() string { return "0.8.0" }

// Tools 返回插件提供的工具列表
func (p *Plugin) Tools() []ap.Tool {
	return []ap.Tool{p.tool}
}

// Init 初始化插件（Git 工具无需额外配置）
func (p *Plugin) Init(config map[string]any) error { return nil }

// Close 关闭插件资源
func (p *Plugin) Close() error { return nil }

// GitTool 是 Git 版本控制操作工具
type GitTool struct{}

// Name 返回工具名称
func (t *GitTool) Name() string { return "git_tool" }

// Description 返回工具描述
func (t *GitTool) Description() string {
	return `Git 版本控制操作工具，支持 clone/status/log/diff/commit/branch/tag/push 等操作。

功能：
- status: 查看工作区状态
- log: 查看提交历史
- diff: 查看未暂存的变更
- branch: 列出/创建/删除分支
- commit: 提交暂存区的变更
- add: 暂存文件
- tag: 创建标签（提供 message 时为附注标签）
- push: 推送分支/标签到远程仓库

参数：
- action (required): 操作类型 [status|log|diff|branch|commit|add|tag|push]
- args: Git 命令参数（如 ["--oneline", "-5"] 用于 log，["main", "--tags"] 用于 push）
- message: 提交消息（commit 操作必需）或标签附注（tag 操作可选）
- name: 标签名（tag 操作必需，如 v1.0.0）
- remote: 远程仓库名（push 操作可选，默认 origin）
- workdir: Git 仓库目录（默认当前目录）`
}

// Parameters 返回工具参数的 JSON Schema
func (t *GitTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {"type": "string", "enum": ["status", "log", "diff", "branch", "commit", "add", "tag", "push"]},
			"args": {"type": "array", "items": {"type": "string"}, "description": "Git 命令参数"},
			"message": {"type": "string", "description": "提交消息（commit 必需）或标签附注（tag 可选）"},
			"name": {"type": "string", "description": "标签名（tag 操作必需，如 v1.0.0）"},
			"remote": {"type": "string", "description": "远程仓库名（push 操作可选，默认 origin）"},
			"workdir": {"type": "string", "description": "Git 仓库目录（默认当前目录）"}
		},
		"required": ["action"]
	}`)
}

// Category 返回工具分类
func (t *GitTool) Category() string { return "vcs" }

// Execute 执行 Git 操作
func (t *GitTool) Execute(ctx context.Context, input json.RawMessage) (*ap.ToolResult, error) {
	var params map[string]any
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("解析参数错误: %w", err)
	}

	action, _ := params["action"].(string)
	if action == "" {
		return ap.NewToolErrorResult("参数 'action' 不能为空"), nil
	}

	workdir, _ := params["workdir"].(string)
	argsAny, _ := params["args"].([]any)
	args := make([]string, 0, len(argsAny))
	for _, a := range argsAny {
		if s, ok := a.(string); ok {
			args = append(args, s)
		}
	}

	var gitArgs []string
	switch action {
	case "status":
		gitArgs = append([]string{"status", "--short"}, args...)
	case "log":
		gitArgs = append([]string{"log"}, args...)
	case "diff":
		gitArgs = append([]string{"diff"}, args...)
	case "branch":
		if len(args) == 0 {
			gitArgs = []string{"branch", "--list"}
		} else {
			gitArgs = append([]string{"branch"}, args...)
		}
	case "commit":
		message, _ := params["message"].(string)
		if message == "" {
			return ap.NewToolErrorResult("commit 操作需要 'message' 参数"), nil
		}
		gitArgs = append([]string{"commit", "-m", message}, args...)
	case "add":
		if len(args) == 0 {
			return ap.NewToolErrorResult("add 操作需要 'args' 参数指定要暂存的文件"), nil
		}
		gitArgs = append([]string{"add"}, args...)
	case "tag":
		// 创建标签：name 必填；提供 message 时创建附注标签，否则为轻量标签
		name, _ := params["name"].(string)
		if name == "" {
			return ap.NewToolErrorResult("tag 操作需要 'name' 参数指定标签名"), nil
		}
		message, _ := params["message"].(string)
		if message != "" {
			gitArgs = append([]string{"tag", "-a", name, "-m", message}, args...)
		} else {
			gitArgs = append([]string{"tag", name}, args...)
		}
	case "push":
		// 推送到远程：remote 缺省为 origin，args 传 refspec 与附加参数（如 main --tags）
		remote, _ := params["remote"].(string)
		if remote == "" {
			remote = "origin"
		}
		gitArgs = append([]string{"push", remote}, args...)
	default:
		return ap.NewToolErrorResult(fmt.Sprintf("未知操作: %s，支持 status/log/diff/branch/commit/add/tag/push", action)), nil
	}

	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	if workdir != "" {
		cmd.Dir = workdir
	}
	output, err := cmd.CombinedOutput()

	result := map[string]any{
		"action":    action,
		"output":    strings.TrimSpace(string(output)),
		"exit_code": cmd.ProcessState.ExitCode(),
	}
	if err != nil && cmd.ProcessState.ExitCode() != 0 {
		result["error"] = err.Error()
	}

	outputJSON, _ := json.MarshalIndent(result, "", "  ")
	return &ap.ToolResult{Content: string(outputJSON)}, nil
}
