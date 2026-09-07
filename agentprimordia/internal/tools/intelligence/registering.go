// registering.go — RegisteringCreator：将工具智能生成的工具自动注册到 Agent Registry
package intelligence

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"agentprimordia/internal/tools"
)

// RegisteringCreator 包装基础 ToolCreator，在创建工具后自动注册到 Registry。
// 将 bench 测试中验证过的 registeringCreator 模式正式化。
type RegisteringCreator struct {
	base ToolCreator
	reg  *tools.Registry
	dir  string // 工作目录，脚本写入 <dir>/.intel-tools/
}

// NewRegisteringCreator 构造 RegisteringCreator
func NewRegisteringCreator(base ToolCreator, reg *tools.Registry, dir string) *RegisteringCreator {
	return &RegisteringCreator{base: base, reg: reg, dir: dir}
}

// Create 调用基础生成器创建工具，然后将脚本写入磁盘并注册到 Registry
func (c *RegisteringCreator) Create(ctx context.Context, gap GapCandidate) (*ToolArtifact, error) {
	// 调用基础生成器
	art, err := c.base.Create(ctx, gap)
	if err != nil {
		return nil, fmt.Errorf("基础生成器创建失败: %w", err)
	}
	if art == nil {
		return nil, nil
	}

	// 确保 .intel-tools 目录存在
	toolsDir := filepath.Join(c.dir, ".intel-tools")
	if err := os.MkdirAll(toolsDir, 0755); err != nil {
		return nil, fmt.Errorf("创建工具目录失败: %w", err)
	}

	// 将工件脚本写入磁盘（可执行权限）
	scriptPath := filepath.Join(toolsDir, art.Name)
	if err := os.WriteFile(scriptPath, art.Artifact, 0755); err != nil {
		return nil, fmt.Errorf("写入工具脚本失败: %w", err)
	}

	// 构造适配 tools.Tool 接口的包装并注册
	t := &artifactTool{
		name:    art.Name,
		desc:    art.Description,
		path:    scriptPath,
		workdir: c.dir,
	}
	if err := c.reg.Register(t); err != nil {
		return nil, fmt.Errorf("注册工具失败: %w", err)
	}

	return art, nil
}

// artifactTool 将 ToolArtifact 适配为 tools.Tool 接口（未导出）
type artifactTool struct {
	name    string
	desc    string
	path    string
	workdir string
}

func (t *artifactTool) Name() string        { return t.name }
func (t *artifactTool) Description() string  { return t.desc }

// Parameters 返回简单的 JSON Schema，包含单个 "args" 字符串参数
func (t *artifactTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"args":{"type":"string","description":"传递给工具的参数"}},"required":["args"]}`)
}

// Execute 通过 sh 执行脚本，args 为传入参数
func (t *artifactTool) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var params struct {
		Args string `json:"args"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult("参数解析失败: " + err.Error()), nil
	}

	// 通过 sh 执行脚本
	cmd := exec.CommandContext(ctx, "sh", t.path)
	for _, arg := range strings.Fields(params.Args) {
		cmd.Args = append(cmd.Args, arg)
	}
	cmd.Dir = t.workdir

	out, err := cmd.CombinedOutput()
	if err != nil {
		return tools.NewResult(fmt.Sprintf("执行出错: %v\n输出: %s", err, string(out))), nil
	}
	return tools.NewResult(strings.TrimSpace(string(out))), nil
}
