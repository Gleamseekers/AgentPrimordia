// coding-agent 演示 AgentPrimordia 的一体化 coding harness：
// 计划(Plan) → 编写(Write) → 实施(Run) → 测试(Test) → 审查(Review) → 发布(Release)
// 全流程由单个 Agent 端到端打通，无需人工接力。
//
// 运行（使用 DemoLLM 脚本化演示，无需 API Key）：
//
//	cd agentprimordia
//	go run ./ecosystem/examples/coding-agent/
//
// 装配要点：
//   - WithCognition：注入 LLMPlanner（首轮任务分解为 DAG）与 LLMReflector（完成路径批评改进）
//   - WithToolkit：filesystem（编写）+ shell（测试/校验）+ git 插件（发布）
//
// 真实使用时仅需把 DemoLLM 替换为 ap.NewOpenAIProvider 等真实 Provider，
// harness 装配与流程完全不变。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/cmd/example/demo"
	gitplugin "github.com/Gleamseekers/AgentPrimordia/agentprimordia/ecosystem/plugins/git"
	ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"
)

// 计划协议：JSON 数组 [{id, description, depends_on}]。
// 根计划分解为 编写→实施→测试→审查→发布 依赖链。
const pipelinePlan = `[
  {"id": "1", "description": "编写：创建 hello.go 文件", "depends_on": []},
  {"id": "2", "description": "实施：运行 go run hello.go 验证可执行", "depends_on": ["1"]},
  {"id": "3", "description": "测试：检查工作区确认文件已生成", "depends_on": ["2"]},
  {"id": "4", "description": "审查：评估代码质量并给出结论", "depends_on": ["3"]},
  {"id": "5", "description": "发布：git 提交并打标签 v1.0.0", "depends_on": ["4"]}
]`

// 子任务再次进入 runLoop 时的计划应答：仅 1 个子任务 → 引擎降级为普通 ReAct 循环，
// 避免无限递归分解。
const singleSubtaskPlan = `[{"id": "1", "description": "直接执行当前步骤", "depends_on": []}]`

// 批评协议：severity low 低于阈值 high，不触发改写，保证最终输出稳定。
const lowCritique = `{"issues": [], "severity": "low", "corrections": []}`

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// 1. 准备临时工作区（真实场景替换为项目目录）
	workdir, err := os.MkdirTemp("", "coding-agent-*")
	if err != nil {
		log.Fatalf("创建工作区失败: %v", err)
	}
	defer os.RemoveAll(workdir)
	fmt.Printf("工作区: %s\n", workdir)

	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "coding-agent@example.com"},
		{"config", "user.name", "coding-agent"},
	} {
		runGit(workdir, args...)
	}

	// 2. 脚本化 LLM（真实场景替换为 ap.NewOpenAIProvider 等）
	provider := scriptLLM(workdir)

	// 3. 工具注册：编写(filesystem) + 测试(shell) + 发布(git)
	registry := ap.NewToolRegistry()
	fs, err := ap.NewFileSystem(workdir)
	if err != nil {
		log.Fatalf("创建 filesystem 工具失败: %v", err)
	}
	if err := registry.Register(fs); err != nil {
		log.Fatalf("注册 filesystem 失败: %v", err)
	}
	if err := registry.Register(ap.NewShell()); err != nil {
		log.Fatalf("注册 shell 失败: %v", err)
	}
	if err := registry.RegisterPlugin(gitplugin.New()); err != nil {
		log.Fatalf("注册 git 插件失败: %v", err)
	}

	// 4. 装配一体化 harness：认知（Planner+Reflector）+ 工具
	agent, err := ap.NewAgent("coding-agent",
		"你是一个全自动编码 Agent，按计划完成编写、测试、审查与发布。",
		provider,
		ap.WithMaxTurns(8),
		ap.WithToolkit(registry),
		ap.WithCognition(ap.CognitionConfig{
			Planner:                     ap.NewLLMPlanner(provider),
			Reflector:                   ap.NewLLMReflector(provider),
			ReflectionSeverityThreshold: "high",
		}),
	)
	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	fmt.Println("目标: 创建 hello.go，运行验证，检查工作区，审查代码，然后提交并打标签 v1.0.0")
	fmt.Println()

	resp, err := agent.Run(ctx, ap.UserMessage("创建 hello.go，运行验证可执行，检查工作区，审查代码，然后提交并打标签 v1.0.0"))
	if err != nil {
		log.Fatalf("Agent 运行失败: %v", err)
	}

	// 5. 输出结果与产物校验
	fmt.Printf("最终结论: %s\n", resp.Content)
	fmt.Printf("消耗轮次: %d | 工具调用: %d\n\n", resp.Metrics.TotalTurns, resp.Metrics.TotalTools)

	content, err := os.ReadFile(filepath.Join(workdir, "hello.go"))
	if err != nil {
		log.Fatalf("产物校验失败: hello.go 不存在: %v", err)
	}
	fmt.Println("产物 hello.go:")
	fmt.Println(strings.TrimSpace(string(content)))

	// 实施环节校验：程序真实可运行
	run := exec.Command("go", "run", "hello.go")
	run.Dir = workdir
	if out, err := run.CombinedOutput(); err != nil {
		log.Fatalf("go run hello.go 失败: %v\n%s", err, out)
	} else {
		fmt.Printf("\ngo run 输出: %s", strings.TrimSpace(string(out)))
	}

	fmt.Printf("\ngit log: %s\n", strings.TrimSpace(runGit(workdir, "log", "--oneline")))
	fmt.Printf("git tag: %s\n", strings.TrimSpace(runGit(workdir, "tag", "-l")))
}

// runGit 在工作区执行 git 命令并返回输出
func runGit(workdir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("git %s 失败: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// jsonString 将字符串编码为 JSON 字符串字面量（含引号），用于内嵌路径
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// scriptLLM 用 DemoLLM 脚本化模拟真实 LLM 的全流程决策。
// 队列时序（每个子任务）：
//   - Complete 队列：单子任务计划 → 结论 → Critique
//   - CallTools 队列：若干 tool 调用轮 + 末尾空响应（回退 Complete 给出结论）
func scriptLLM(workdir string) *demo.DemoLLM {
	d := demo.NewDemoLLM()
	d.WithResponse(pipelinePlan) // 根任务 Planning：分解为 5 个子任务

	fileContent := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello from AgentPrimordia coding harness!\")\n}\n"

	// 子任务1：编写（filesystem.write）
	subtask(d, "hello.go 已创建", [][]ap.FunctionCall{{
		{ID: "call_write", Name: "filesystem",
			Arguments: `{"action": "write", "path": "hello.go", "content": ` + jsonString(fileContent) + `}`},
	}})

	// 子任务2：实施（shell 实际运行程序）
	subtask(d, "运行成功：Hello from AgentPrimordia coding harness!", [][]ap.FunctionCall{{
		{ID: "call_run", Name: "shell",
			Arguments: `{"action": "execute", "command": "go run hello.go", "workdir": ` + jsonString(workdir) + `}`},
	}})

	// 子任务3：测试（shell 校验工作区）
	subtask(d, "工作区检查通过：hello.go 已生成", [][]ap.FunctionCall{{
		{ID: "call_check", Name: "shell",
			Arguments: `{"action": "execute", "command": "git status --short", "workdir": ` + jsonString(workdir) + `}`},
	}})

	// 子任务4：审查（纯推理，无工具）
	subtask(d, "审查通过：代码结构清晰，无高严重度问题", nil)

	// 子任务5：发布（git add+commit 同轮双调用，再 tag）
	subtask(d, "发布完成：v1.0.0", [][]ap.FunctionCall{
		{
			{ID: "call_add", Name: "git_tool",
				Arguments: `{"action": "add", "args": ["."], "workdir": ` + jsonString(workdir) + `}`},
			{ID: "call_commit", Name: "git_tool",
				Arguments: `{"action": "commit", "message": "feat: add hello.go", "workdir": ` + jsonString(workdir) + `}`},
		},
		{
			{ID: "call_tag", Name: "git_tool",
				Arguments: `{"action": "tag", "name": "v1.0.0", "message": "release v1.0.0", "workdir": ` + jsonString(workdir) + `}`},
		},
	})

	return d
}

// subtask 按子任务时序向两条队列追加脚本响应
func subtask(d *demo.DemoLLM, conclusion string, toolRounds [][]ap.FunctionCall) {
	d.WithResponse(singleSubtaskPlan)
	for _, round := range toolRounds {
		d.WithToolResponse(round)
	}
	d.WithToolResponse(nil) // 空 tool 响应 → 引擎回退 Complete 给出结论
	d.WithResponse(conclusion)
	d.WithResponse(lowCritique)
}
