// Package e2e 提供 AgentPrimordia 端到端集成测试。
//
// coding_pipeline_test.go 验证「一体化 coding harness」全流程：
// 计划（Planner 分解 DAG）→ 编写（filesystem 写文件）→ 实施（shell 运行程序）
// → 测试（shell 校验）→ 审查（Reflector 批评最终输出）→ 发布（git add/commit/tag），
// 全部由 MockLLM 脚本化驱动，不依赖真实 LLM 与网络。
package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitplugin "github.com/Gleamseekers/AgentPrimordia/ecosystem/plugins/git"
	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

// singleSubtaskPlan 是子任务 runLoop 再次触发 Planning 时的应答：
// 仅含 1 个子任务时引擎不再走 DAG 分支（需 >1 才 executePlan），
// 避免无限递归分解。
const singleSubtaskPlan = `[{"id": "1", "description": "直接执行当前步骤", "depends_on": []}]`

// lowCritique 是 Reflector.Critique 的应答：严重度 low 低于阈值 high，
// 不触发 Improve 二次改写，保证最终输出可断言。
const lowCritique = `{"issues": [], "severity": "low", "corrections": []}`

// pipelinePlan 是根任务的分解计划：编写 → 实施 → 测试 → 审查 → 发布 依赖链
const pipelinePlan = `[
  {"id": "1", "description": "编写：创建 hello.go 文件", "depends_on": []},
  {"id": "2", "description": "实施：运行 go run hello.go 验证可执行", "depends_on": ["1"]},
  {"id": "3", "description": "测试：检查工作区确认文件已生成", "depends_on": ["2"]},
  {"id": "4", "description": "审查：评估代码质量并给出结论", "depends_on": ["3"]},
  {"id": "5", "description": "发布：git 提交并打标签 v1.0.0", "depends_on": ["4"]}
]`

// initGitRepo 在指定目录初始化带用户配置的 Git 仓库
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "e2e@example.com"},
		{"config", "user.name", "e2e-bot"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
}

// gitOutput 在目录中执行 git 命令并返回输出
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

// mustJSONString 将字符串编码为 JSON 字符串字面量（含引号），
// 用于在 tool 参数中内嵌文件内容/路径
func mustJSONString(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			sb.WriteRune(r)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// scriptSubtask 按子任务时序追加 Mock 响应：
// Complete 队列 = 计划检查（单子任务降级）→ 结论文本 → Critique；
// CallTools 队列 = 若干 tool 调用轮 + 末尾一个空响应（触发回退 Complete 给出结论）。
func scriptSubtask(mock *llm.MockLLM, conclusion string, toolRounds [][]llm.FunctionCall) {
	mock.WithResponse(singleSubtaskPlan)
	for _, round := range toolRounds {
		mock.WithToolResponse(round)
	}
	mock.WithToolResponse(nil) // 空 tool 响应 → syncReasoning 回退 Complete
	mock.WithResponse(conclusion)
	mock.WithResponse(lowCritique)
}

// TestCodingPipeline_EndToEnd 验证计划→编写→实施→测试→审查→发布全流程打通。
//
// 执行时序（每个子任务都会重新进入 runLoop，startTurn=0 再次触发 Planning，
// 用单子任务计划使其降级为普通 ReAct 循环）：
//
//	根 runLoop: Planner 分解为 5 子任务 → executePlan DAG 分层执行
//	子任务1 编写: filesystem.write
//	子任务2 实施: shell(go run hello.go) 实际运行程序
//	子任务3 测试: shell(git status)
//	子任务4 审查: 无 tool 直接给结论
//	子任务5 发布: git add + commit（同一轮两个调用）→ git tag
//	每个子任务完成路径: Reflector.Critique（low → 不改写）
func TestCodingPipeline_EndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	workdir := t.TempDir()
	initGitRepo(t, workdir)

	mock := llm.NewMockLLM(t)
	mock.WithResponse(pipelinePlan) // 根任务 Planning：分解为 5 个子任务

	fileContent := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello from AgentPrimordia coding harness!\")\n}\n"

	// 子任务1：编写（filesystem 写文件）
	scriptSubtask(mock, "已创建 hello.go", [][]llm.FunctionCall{{
		{
			ID:        "call_write",
			Name:      "filesystem",
			Arguments: `{"action": "write", "path": "hello.go", "content": ` + mustJSONString(fileContent) + `}`,
		},
	}})

	// 子任务2：实施（shell 实际运行程序，验证可执行）
	scriptSubtask(mock, "运行成功：Hello from AgentPrimordia coding harness!", [][]llm.FunctionCall{{
		{
			ID:        "call_run",
			Name:      "shell",
			Arguments: `{"action": "execute", "command": "go run hello.go", "workdir": ` + mustJSONString(workdir) + `}`,
		},
	}})

	// 子任务3：测试（shell 检查工作区）
	scriptSubtask(mock, "工作区检查通过", [][]llm.FunctionCall{{
		{
			ID:        "call_check",
			Name:      "shell",
			Arguments: `{"action": "execute", "command": "git status --short", "workdir": ` + mustJSONString(workdir) + `}`,
		},
	}})

	// 子任务4：审查（无 tool，直接结论）
	scriptSubtask(mock, "审查通过：代码结构清晰", nil)

	// 子任务5：发布（git add+commit 同轮双调用，再 tag）
	scriptSubtask(mock, "发布完成：v1.0.0", [][]llm.FunctionCall{
		{
			{
				ID:        "call_add",
				Name:      "git_tool",
				Arguments: `{"action": "add", "args": ["."], "workdir": ` + mustJSONString(workdir) + `}`,
			},
			{
				ID:        "call_commit",
				Name:      "git_tool",
				Arguments: `{"action": "commit", "message": "feat: add hello.go", "workdir": ` + mustJSONString(workdir) + `}`,
			},
		},
		{
			{
				ID:        "call_tag",
				Name:      "git_tool",
				Arguments: `{"action": "tag", "name": "v1.0.0", "message": "release v1.0.0", "workdir": ` + mustJSONString(workdir) + `}`,
			},
		},
	})

	// ===== 装配 Agent：Planner + Reflector + filesystem/shell/git 工具 =====
	registry := ap.NewToolRegistry()
	fs, err := ap.NewFileSystem(workdir)
	if err != nil {
		t.Fatalf("创建 filesystem 工具失败: %v", err)
	}
	if err := registry.Register(fs); err != nil {
		t.Fatalf("注册 filesystem 失败: %v", err)
	}
	if err := registry.Register(ap.NewShell()); err != nil {
		t.Fatalf("注册 shell 失败: %v", err)
	}
	if err := registry.RegisterPlugin(gitplugin.New()); err != nil {
		t.Fatalf("注册 git 插件失败: %v", err)
	}

	codingAgent, err := ap.NewAgent("coding-agent",
		"你是一个全自动编码 Agent，按计划完成编写、测试、审查与发布。",
		mock,
		ap.WithMaxTurns(8),
		ap.WithToolkit(registry),
		ap.WithCognition(ap.CognitionConfig{
			Planner:                     ap.NewLLMPlanner(mock),
			Reflector:                   ap.NewLLMReflector(mock),
			ReflectionSeverityThreshold: "high",
		}),
	)
	if err != nil {
		t.Fatalf("创建 Agent 失败: %v", err)
	}

	resp, err := codingAgent.Run(context.Background(),
		ap.UserMessage("创建 hello.go，验证工作区，审查代码，然后提交并打标签 v1.0.0"))
	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	// 最终输出 = 最后一个子任务（发布）的结论
	if resp.Content != "发布完成：v1.0.0" {
		t.Errorf("最终输出 = %q, want %q", resp.Content, "发布完成：v1.0.0")
	}

	// 编写环节：文件已落盘且内容正确
	got, err := os.ReadFile(filepath.Join(workdir, "hello.go"))
	if err != nil {
		t.Fatalf("hello.go 未创建: %v", err)
	}
	if string(got) != fileContent {
		t.Errorf("hello.go 内容 = %q, want %q", string(got), fileContent)
	}

	// 实施环节：程序真实可执行且输出符合预期
	run := exec.Command("go", "run", "hello.go")
	run.Dir = workdir
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("go run hello.go 失败: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Hello from AgentPrimordia coding harness!") {
		t.Errorf("go run 输出 = %q, want 包含 Hello from AgentPrimordia coding harness!", string(out))
	}

	// 发布环节：提交与标签进入仓库
	if log := gitOutput(t, workdir, "log", "--oneline"); !strings.Contains(log, "feat: add hello.go") {
		t.Errorf("git log 缺少提交, got: %s", log)
	}
	if tags := gitOutput(t, workdir, "tag", "--list"); !strings.Contains(tags, "v1.0.0") {
		t.Errorf("git tag 缺少 v1.0.0, got: %s", tags)
	}

	// 指标：共执行 6 次 tool 调用（write/run/check/add/commit/tag）
	if resp.Metrics.TotalTools != 6 {
		t.Errorf("TotalTools = %d, want 6", resp.Metrics.TotalTools)
	}
}
