package builtin

import (
	"fmt"
	"os"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

// BuiltinPlugin 是内置tool插件，实现 tools.ToolPlugin 接口。
// 它将 FileSystem、Shell、Web、Calculator、DateTime 等tool打包为插件。
type BuiltinPlugin struct {
	tools []tools.Tool
}

func (p *BuiltinPlugin) Name() string    { return "builtin" }
func (p *BuiltinPlugin) Version() string { return "1.0.0" }

func (p *BuiltinPlugin) Tools() []tools.Tool {
	return p.tools
}

func (p *BuiltinPlugin) Init(config map[string]any) error {
	rootDir, _ := config["root_dir"].(string)
	if rootDir == "" {
		return fmt.Errorf("root_dir is required")
	}

	enableFS, _ := config["enable_fs"].(bool)
	enableShell, _ := config["enable_shell"].(bool)
	enableWeb, _ := config["enable_web"].(bool)
	enableUtils, _ := config["enable_utils"].(bool)

	p.tools = nil

	if enableFS {
		fs, err := NewFileSystem(rootDir)
		if err != nil {
			return fmt.Errorf("filesystem init failed: %w", err)
		}
		p.tools = append(p.tools, fs)
	}

	if enableShell {
		p.tools = append(p.tools, NewShell())
	}

	if enableWeb {
		p.tools = append(p.tools, NewWeb())
	}

	if enableUtils {
		p.tools = append(p.tools, NewCalculator())
		p.tools = append(p.tools, NewDateTime())
	}

	return nil
}

func (p *BuiltinPlugin) Close() error {
	p.tools = nil
	return nil
}

// --- 辅助函数（供测试直接使用） ---

func NewBuiltinFS(rootDir string) (tools.Tool, error) {
	info, err := os.Stat(rootDir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", rootDir)
	}
	return NewFileSystem(rootDir)
}

func NewBuiltinShell() tools.Tool {
	return NewShell()
}

func NewBuiltinWeb() tools.Tool {
	return NewWeb()
}

func NewBuiltinCalc() tools.Tool {
	return NewCalculator()
}

func NewBuiltinDateTime() tools.Tool {
	return NewDateTime()
}
