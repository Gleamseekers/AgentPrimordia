package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

// MCPToolAdapter 将 MCP tool适配为 AP 的 tools.Tool 接口
type MCPToolAdapter struct {
	client  *Client
	toolDef ToolDefinition
}

// NewMCPToolAdapter 创建 MCP tool适配器
func NewMCPToolAdapter(client *Client, toolDef ToolDefinition) *MCPToolAdapter {
	return &MCPToolAdapter{
		client:  client,
		toolDef: toolDef,
	}
}

// Name 实现 tools.Tool 接口，返回tool名称
func (a *MCPToolAdapter) Name() string {
	return a.toolDef.Name
}

// Description 实现 tools.Tool 接口，返回tool描述
func (a *MCPToolAdapter) Description() string {
	return a.toolDef.Description
}

// Parameters 实现 tools.Tool 接口，返回tool参数的 JSON Schema
func (a *MCPToolAdapter) Parameters() json.RawMessage {
	if a.toolDef.InputSchema != nil {
		raw, err := json.Marshal(a.toolDef.InputSchema)
		if err == nil {
			return raw
		}
	}
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

// Execute 实现 tools.Tool 接口，调用 MCP tool并返回结果
func (a *MCPToolAdapter) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		// 无效 JSON 参数，使用空 map
		argsMap = make(map[string]any)
	}

	mcpResult, err := a.client.CallTool(ctx, a.toolDef.Name, argsMap)
	if err != nil {
		return tools.NewErrorResult(err.Error()), err
	}

	// 合并所有文本内容，并限制总大小
	var textParts []string
	totalLen := 0
	for _, content := range mcpResult.Content {
		if content.Type == "text" && content.Text != "" {
			text := content.Text
			remaining := maxToolResultLen - totalLen
			if remaining <= 0 {
				break
			}
			if len(text) > remaining {
				text = text[:remaining] + "\n... [MCP 结果已截断]"
			}
			textParts = append(textParts, text)
			totalLen += len(text)
		}
	}

	resultText := strings.Join(textParts, "\n")
	if mcpResult.IsError {
		return tools.NewErrorResult(resultText), fmt.Errorf("MCP tool %q returned error", a.toolDef.Name)
	}
	return tools.NewResult(resultText), nil
}
