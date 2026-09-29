package sqlplugin

import (
	"fmt"

	ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"
)

// Plugin 是 SQLite 数据库插件，封装 ap.SQLiteTool
type Plugin struct {
	tool *ap.SQLiteTool
}

// New 创建新的 SQL 插件实例（工具在 Init 中创建）
func New() *Plugin {
	return &Plugin{}
}

// Name 返回插件名称
func (p *Plugin) Name() string { return "sql" }

// Version 返回插件版本
func (p *Plugin) Version() string { return "0.1.0" }

// Tools 返回插件提供的工具列表
func (p *Plugin) Tools() []ap.Tool {
	if p.tool == nil {
		return nil
	}
	return []ap.Tool{p.tool}
}

// Init 初始化插件，从 config 中读取 db_path 创建 SQLiteTool
func (p *Plugin) Init(config map[string]any) error {
	dbPath := "ap.db"
	if path, ok := config["db_path"].(string); ok && path != "" {
		dbPath = path
	}

	tool, err := ap.NewSQLiteTool(dbPath)
	if err != nil {
		return fmt.Errorf("创建 SQLiteTool 失败: %w", err)
	}
	p.tool = tool
	return nil
}

// Close 关闭插件资源，释放数据库连接
func (p *Plugin) Close() error {
	if p.tool != nil {
		return p.tool.Close()
	}
	return nil
}
