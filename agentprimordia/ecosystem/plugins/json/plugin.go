package jsonplugin

import (
	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

// Plugin 是 JSON/CSV 数据处理插件，封装 ap.JSONTool 和 ap.CSVTool
type Plugin struct {
	jsonTool *ap.JSONTool
	csvTool  *ap.CSVTool
}

// New 创建新的 JSON 插件实例
func New() *Plugin {
	return &Plugin{
		jsonTool: ap.NewJSONTool(),
		csvTool:  ap.NewCSVTool(),
	}
}

// Name 返回插件名称
func (p *Plugin) Name() string { return "json" }

// Version 返回插件版本
func (p *Plugin) Version() string { return "0.1.0" }

// Tools 返回插件提供的工具列表（JSON 和 CSV 处理器）
func (p *Plugin) Tools() []ap.Tool {
	return []ap.Tool{p.jsonTool, p.csvTool}
}

// Init 初始化插件（JSON/CSV 工具无需额外配置）
func (p *Plugin) Init(config map[string]any) error { return nil }

// Close 关闭插件资源
func (p *Plugin) Close() error { return nil }
