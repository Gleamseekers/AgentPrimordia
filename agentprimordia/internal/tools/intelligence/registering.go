// registering.go — RegisteringCreator：将工具智能生成的工具注册到 Registry
//
// INV-0 安全边界（AGENTS.md §2.3，P1 加固）：
//
//	宿主进程运行期零写入、零编译、零加载任何 agent 生成的代码；
//	agent 生成代码唯一合法执行位置是 wazero WASM 沙箱。
//
// 修复前本类型将 LLM/缺口检测生成的 artifact 以 0755 写入磁盘并注册为
// 经 `sh <path>` 执行的工具——正是 INV-0 禁止的形态。修复后：
//   - 未配置验签器（ArtifactVerifier）→ 一律拒绝注册（fail-closed）；
//   - 工件哈希未锚定（ArtifactSHA 为空）或与内容不符 → 拒绝；
//   - 验签未通过 → 拒绝；
//   - 全部通过后才以 0644（数据文件，无可执行位）落盘并注册；
//   - 注册工具的 Execute 绝不派生宿主进程，仅可经 WithExecutor 注入的
//     执行器（唯一合法实现：github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/wasm 沙箱适配器）执行。
package intelligence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

// ArtifactVerifier 工件验签门（INV-0/A6 门控接口）。
// 实现方应校验工件签名（如 lifecycle.TrustChain：锚定哈希 + 钉扎公钥 +
// cosign/ed25519 验签）。未注入或验签失败时 RegisteringCreator 拒绝注册。
type ArtifactVerifier interface {
	VerifyArtifact(art *ToolArtifact) error
}

// ArtifactExecutor 工件执行器注入点。
// 唯一合法实现是 wazero WASM 沙箱（github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/wasm.WASMToolAdapter），
// 由组装根（cmd/ 或测试）绑定；宿主进程禁止直接执行 agent 生成代码。
// args 为工具调用参数 JSON。
type ArtifactExecutor func(ctx context.Context, art *ToolArtifact, args json.RawMessage) (*tools.Result, error)

// RegisteringCreator 包装基础 ToolCreator，在创建工具后按 INV-0 门控
// 注册到 Registry。将 bench 测试中验证过的 registeringCreator 模式正式化。
type RegisteringCreator struct {
	base     ToolCreator
	reg      *tools.Registry
	dir      string // 工作目录，工件写入 <dir>/.intel-tools/
	verifier ArtifactVerifier
	executor ArtifactExecutor
}

// NewRegisteringCreator 构造 RegisteringCreator。
// 默认不注入验签器/执行器——此时 Create 对任何工件一律拒绝注册
// （安全默认值）；经 WithVerifier 注入验签门后方可注册。
func NewRegisteringCreator(base ToolCreator, reg *tools.Registry, dir string) *RegisteringCreator {
	return &RegisteringCreator{base: base, reg: reg, dir: dir}
}

// WithVerifier 注入工件验签门（INV-0 门控必填项）。
func (c *RegisteringCreator) WithVerifier(v ArtifactVerifier) *RegisteringCreator {
	c.verifier = v
	return c
}

// WithExecutor 注入工件执行器（唯一合法实现：wasm 沙箱，由组装根绑定）。
// 未注入时注册成功的工具在 Execute 阶段仍拒绝执行（宿主零执行）。
func (c *RegisteringCreator) WithExecutor(e ArtifactExecutor) *RegisteringCreator {
	c.executor = e
	return c
}

// Create 调用基础生成器创建工具，然后按 INV-0 门控决定是否注册。
// 门控未通过时返回错误且不注册、不落盘（fail-closed）。
func (c *RegisteringCreator) Create(ctx context.Context, gap GapCandidate) (*ToolArtifact, error) {
	// 调用基础生成器
	art, err := c.base.Create(ctx, gap)
	if err != nil {
		return nil, fmt.Errorf("基础生成器创建失败: %w", err)
	}
	if art == nil {
		return nil, nil
	}

	if err := c.register(art); err != nil {
		return nil, err
	}
	return art, nil
}

// register 执行 INV-0 门控并注册工件（全部先验后写）。
func (c *RegisteringCreator) register(art *ToolArtifact) error {
	// 1. 名称净化：必须是单个安全路径段（拒 ".."、分隔符、绝对形态）
	if !isSafeToolName(art.Name) {
		return fmt.Errorf("intelligence: 工具名 %q 不是单段安全名，拒绝注册", art.Name)
	}

	// 2. 完整性锚定：ArtifactSHA 必填且必须与工件字节一致（防签名对象
	// 与工件错位、防生成后被篡改）
	if art.ArtifactSHA == "" {
		return fmt.Errorf("intelligence: 工件 %q 未锚定 sha256，拒绝注册", art.Name)
	}
	sum := sha256.Sum256(art.Artifact)
	if hex.EncodeToString(sum[:]) != art.ArtifactSHA {
		return fmt.Errorf("intelligence: 工件 %q 哈希与锚定值不符（疑似篡改），拒绝注册", art.Name)
	}

	// 3. INV-0 验签门：未配置验签器或验签失败 → 拒绝
	if c.verifier == nil {
		return fmt.Errorf("intelligence: 未配置工件验签器，拒绝注册 %q"+
			"（INV-0：宿主不得写入/加载 agent 生成代码；唯一合法通道为 lifecycle 验签 + wasm 沙箱）", art.Name)
	}
	if err := c.verifier.VerifyArtifact(art); err != nil {
		return fmt.Errorf("intelligence: 工件 %q 验签未通过，拒绝注册: %w", art.Name, err)
	}

	// 4. 门控通过：以 0644（数据文件，无可执行位）落盘
	toolsDir := filepath.Join(c.dir, ".intel-tools")
	if err := os.MkdirAll(toolsDir, 0755); err != nil {
		return fmt.Errorf("创建工具目录失败: %w", err)
	}
	scriptPath := filepath.Join(toolsDir, art.Name)
	if err := os.WriteFile(scriptPath, art.Artifact, 0644); err != nil {
		return fmt.Errorf("写入工具工件失败: %w", err)
	}

	// 5. 注册（执行通道仅可来自注入的 executor——缺省拒绝宿主执行）
	t := &artifactTool{
		name:     art.Name,
		desc:     art.Description,
		path:     scriptPath,
		workdir:  c.dir,
		artifact: art,
		executor: c.executor,
	}
	if err := c.reg.Register(t); err != nil {
		return fmt.Errorf("注册工具失败: %w", err)
	}
	return nil
}

// isSafeToolName 工具名必须是单个安全路径段：
// 拒绝空名、"."、".."、含 ".."、含路径分隔符及一切非常规路径成分。
func isSafeToolName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return false
	}
	return filepath.Base(name) == name
}

// artifactTool 将 ToolArtifact 适配为 tools.Tool 接口（未导出）。
// INV-0：Execute 绝不派生宿主进程执行工件；仅委托注入的 executor
// （唯一合法实现：wasm 沙箱）。未注入 executor 时拒绝执行。
type artifactTool struct {
	name     string
	desc     string
	path     string // 工件数据文件位置（0644，仅供审计/检查）
	workdir  string
	artifact *ToolArtifact
	executor ArtifactExecutor
}

func (t *artifactTool) Name() string       { return t.name }
func (t *artifactTool) Description() string { return t.desc }

// Parameters 返回简单的 JSON Schema，包含单个 "args" 字符串参数
func (t *artifactTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"args":{"type":"string","description":"传递给工具的参数"}},"required":["args"]}`)
}

// Execute 执行工具。
// INV-0 边界：agent 生成的工件不得在宿主进程直接执行——无 executor
// （wasm 沙箱通道）时一律拒绝；有 executor 时委托执行。
func (t *artifactTool) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var params struct {
		Args string `json:"args"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult("参数解析失败: " + err.Error()), nil
	}

	if t.executor == nil {
		return tools.NewErrorResult(fmt.Sprintf(
			"工具 %q 的执行被拒绝：agent 生成的工件不得在宿主进程直接执行（INV-0）。"+
				"唯一合法执行通道为 wazero WASM 沙箱（经 RegisteringCreator.WithExecutor 注入）。",
			t.name)), nil
	}
	return t.executor(ctx, t.artifact, args)
}
